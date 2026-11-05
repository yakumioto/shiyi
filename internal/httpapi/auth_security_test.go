package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"shiyi/internal/api"
	"shiyi/internal/auth"
	"shiyi/internal/classify"
	"shiyi/internal/llm"
)

func TestEveryMutationRequiresOriginBoundCSRFAndJSON(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	variants := []struct {
		name   string
		change func(*http.Request)
		want   int
	}{
		{"missing origin", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"deceptive suffix", func(r *http.Request) { r.Header.Set("Origin", "https://memory.example.attacker.test") }, 403},
		{"wrong port", func(r *http.Request) { r.Header.Set("Origin", "https://memory.example:444") }, 403},
		{"wrong scheme", func(r *http.Request) { r.Header.Set("Origin", "http://memory.example") }, 403},
		{"multiple origin headers", func(r *http.Request) { r.Header.Add("Origin", h.cfg.PublicOrigin) }, 403},
		{"origin list", func(r *http.Request) { r.Header.Set("Origin", h.cfg.PublicOrigin+" https://other.test") }, 403},
		{"forged proxy", func(r *http.Request) {
			r.Header.Set("Origin", "https://attacker.test")
			r.Header.Set("Forwarded", "host=memory.example;proto=https")
			r.Header.Set("X-Forwarded-Host", "memory.example")
			r.Header.Set("Referer", h.cfg.PublicOrigin)
		}, 403},
		{"cross site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"missing csrf", func(r *http.Request) { r.Header.Del(api.HeaderCSRFToken) }, 403},
		{"wrong csrf", func(r *http.Request) { r.Header.Set(api.HeaderCSRFToken, "forged") }, 403},
		{"duplicate csrf", func(r *http.Request) { r.Header.Add(api.HeaderCSRFToken, b.token) }, 403},
		{"no cookie", func(r *http.Request) { r.Header.Set("Cookie", "unrelated=value") }, 403},
		{"no content type", func(r *http.Request) { r.Header.Del("Content-Type") }, 400},
		{"form", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 400},
		{"text", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400},
	}
	for _, v := range variants {
		if v.name == "no cookie" {
			continue
		}
		t.Run("login/"+v.name, func(t *testing.T) {
			res, raw := b.request("POST", api.PathAuthLogin, `{"password":"wrong"}`, v.change)
			requireStatus(t, res, raw, v.want)
		})
	}
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	for _, path := range []string{"/messages", api.PathAuthLogin, api.PathAuthLogout, api.PathAuthLogoutAll} {
		for _, v := range variants {
			if v.name == "no cookie" {
				continue
			}
			t.Run(path+"/"+v.name, func(t *testing.T) {
				res, raw := b.request("POST", path, `{"password":"wrong","content":"secret"}`, v.change)
				requireStatus(t, res, raw, v.want)
			})
		}
	}
	if h.judge.calls != 0 || h.memory.appendCalls != 0 {
		t.Fatal("rejected mutations reached business")
	}
	res, raw = b.request("POST", "/messages", `{"content":"allowed"}`, func(r *http.Request) {
		r.Header.Set("Origin", "https://MEMORY.EXAMPLE:443")
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
	})
	requireStatus(t, res, raw, 201)
}

func TestBootstrapIsBoundReusableAndExpiresWithoutGrantingAccess(t *testing.T) {
	h := newAuthHarness(t)
	a, b := h.browser(), h.browser()
	first := a.status(false)
	second := a.status(false)
	if first.CSRFToken != second.CSRFToken {
		t.Fatal("bootstrap rotated during polling")
	}
	b.status(false)
	b.token = a.token
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 403)
	res, raw = a.request("POST", "/messages", `{"content":"not authorized"}`, nil)
	requireStatus(t, res, raw, 401)
	// A pair of attacker-chosen matching strings cannot invent a challenge.
	forged := h.browser()
	forged.token = "chosen"
	res, raw = forged.request("POST", api.PathAuthLogin, `{"password":"wrong"}`, func(r *http.Request) { r.Header.Set("Cookie", auth.BootstrapCookieName+"=chosen") })
	requireStatus(t, res, raw, 403)
	h.clock.advance(20 * time.Minute)
	res, raw = a.login(loginPassword)
	requireStatus(t, res, raw, 403)
	a.status(false)
	oldToken := a.token
	res, raw = a.login(loginPassword)
	requireStatus(t, res, raw, 200)
	a.token = oldToken
	res, raw = a.request("POST", "/messages", `{"content":"stale challenge"}`, nil)
	requireStatus(t, res, raw, 403)
	a.status(true)
	res, raw = a.request("POST", "/messages", `{"content":"valid"}`, nil)
	requireStatus(t, res, raw, 201)
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Set("Origin", "https://evil.test") }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }} {
		res, raw = a.request("GET", api.PathAuthSession, "", change)
		requireStatus(t, res, raw, 403)
	}
}

func TestPasswordEndpointsBoundAndValidateJSONBeforeAttempts(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	for i := 0; i < 12; i++ {
		res, raw := b.request("POST", api.PathAuthLogin, "{broken", nil)
		requireStatus(t, res, raw, 400)
	}
	for _, body := range []string{"null", `{"password":null}`, `{"password":"wrong","unexpected":true}`, `{"password":42}`, `{} {}`} {
		res, raw := b.request("POST", api.PathAuthLogin, body, nil)
		requireStatus(t, res, raw, 400)
	}
	res, raw := b.request("POST", api.PathAuthLogin, strings.Repeat(" ", 4097), nil)
	requireStatus(t, res, raw, 413)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	for _, path := range []string{api.PathAuthLogin, api.PathAuthLogout, api.PathAuthLogoutAll} {
		res, raw = b.request("GET", path, "", nil)
		requireStatus(t, res, raw, 405)
		if res.Header.Get("Allow") != "POST" {
			t.Fatal("method response lacks Allow")
		}
	}
	res, raw = b.request("POST", api.PathAuthSession, "", nil)
	requireStatus(t, res, raw, 405)
	res, raw = b.request("GET", "/auth/not-found", "", nil)
	requireStatus(t, res, raw, 404)
}

func TestAuthenticationWorksDuringModelFailureAndNeverLogsSecrets(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	var sessionSecret string
	for _, c := range res.Cookies() {
		if c.Name == auth.SessionCookieName {
			sessionSecret = c.Value
		}
	}
	const original = "original-private-marker"
	const modelKey = "sk-model-private-marker"
	h.judge.err = errors.Join(llm.ErrUnavailable, errors.New(original+" "+modelKey+" "+loginPassword))
	res, raw = b.request("POST", "/messages", `{"content":"`+original+`"}`, nil)
	requireStatus(t, res, raw, 503)
	for _, secret := range []string{original, modelKey, loginPassword, h.cfg.PasswordHash, sessionSecret, b.token} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("failure response disclosed secret")
		}
	}
	if h.memory.appendCalls != 0 {
		t.Fatal("model failure wrote unclassified input")
	}
	b.status(true)
	res, raw = b.request("POST", api.PathAuthLogout, "", nil)
	requireStatus(t, res, raw, 204)
	// Unknown URL/method and invalid model verdict are untrusted too.
	b.request("GET", "/"+modelKey+"?password="+sessionSecret, "", nil)
	b.request("PRIVATE-METHOD-MARKER", "/", "", nil)
	b.status(false)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	h.judge.err = nil
	h.judge.kind = classify.Kind(original)
	res, raw = b.request("POST", "/messages", `{"content":"`+original+`"}`, nil)
	requireStatus(t, res, raw, 500)
	for _, secret := range []string{original, modelKey, loginPassword, h.cfg.PasswordHash, sessionSecret, b.token, "PRIVATE-METHOD-MARKER"} {
		if strings.Contains(h.logs.String(), secret) {
			t.Fatalf("logs leaked private data marker")
		}
	}
	var envelope api.ErrorResponse
	if json.Unmarshal(raw, &envelope) != nil || envelope.Error.Code != api.CodeInternalError {
		t.Fatal("unexpected error shape")
	}
}
