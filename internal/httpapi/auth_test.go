package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shiyi/internal/api"
	"shiyi/internal/auth"
	"shiyi/internal/classify"
	"shiyi/internal/config"
	"shiyi/internal/httpapi"
	"shiyi/internal/store"
)

const loginPassword = " test-only password 密码 "

var passwordHashOnce sync.Once
var passwordHash string

func testPasswordHash(t *testing.T) string {
	t.Helper()
	passwordHashOnce.Do(func() {
		b, err := bcrypt.GenerateFromPassword([]byte(loginPassword), 10)
		if err != nil {
			t.Fatal(err)
		}
		passwordHash = string(b)
	})
	return passwordHash
}

type authClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *authClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *authClock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type authHarness struct {
	t      *testing.T
	cfg    config.AuthConfig
	clock  *authClock
	server *httptest.Server
	app    *httpapi.Server
	judge  *fakeJudge
	memory *fakeStore
	logs   *bytes.Buffer
	wrap   func(http.Handler) http.Handler
}
type browser struct {
	h      *authHarness
	client *http.Client
	token  string
}

func newAuthHarness(t *testing.T) *authHarness {
	t.Helper()
	h := &authHarness{t: t, cfg: config.AuthConfig{PasswordHash: testPasswordHash(t), StateDir: filepath.Join(t.TempDir(), "auth"), PublicOrigin: "https://memory.example"}, clock: &authClock{now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}, judge: &fakeJudge{kind: classify.KindRecord}, memory: &fakeStore{record: store.Record{Version: 1, Time: store.NewTime(recordTime)}}, logs: &bytes.Buffer{}}
	h.start()
	t.Cleanup(func() { h.stop() })
	return h
}
func (h *authHarness) start() {
	h.t.Helper()
	module, err := auth.New(h.cfg, auth.Options{Now: h.clock.Now})
	if err != nil {
		h.t.Fatal(err)
	}
	app, err := httpapi.New(httpapi.Deps{Auth: module, Config: config.Config{DataDir: h.t.TempDir(), Classifier: config.ModelConfig{BaseURL: "http://model.invalid", Model: "test"}, Answer: config.ModelConfig{BaseURL: "http://model.invalid", Model: "test"}}, Judge: h.judge, Store: h.memory, Answerer: &fakeAnswerer{}, Logger: slog.New(slog.NewTextHandler(h.logs, nil))})
	if err != nil {
		module.Close()
		h.t.Fatal(err)
	}
	h.app = app
	handler := app.Handler()
	if h.wrap != nil {
		handler = h.wrap(handler)
	}
	h.server = httptest.NewTLSServer(handler)
}
func (h *authHarness) stop() {
	if h.server != nil {
		h.server.Close()
		if err := h.app.Close(); err != nil {
			h.t.Error(err)
		}
		h.server = nil
	}
}
func (h *authHarness) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{h: h, client: &http.Client{Transport: h.server.Client().Transport, Jar: jar}}
}
func (b *browser) request(method, path, body string, change func(*http.Request)) (*http.Response, []byte) {
	b.h.t.Helper()
	req, err := http.NewRequest(method, b.h.server.URL+path, strings.NewReader(body))
	if err != nil {
		b.h.t.Fatal(err)
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", b.h.cfg.PublicOrigin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(api.HeaderCSRFToken, b.token)
	}
	if change != nil {
		change(req)
	}
	res, err := b.client.Do(req)
	if err != nil {
		b.h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		b.h.t.Fatal(err)
	}
	if strings.HasPrefix(path, "/auth/") || path == "/messages" {
		if res.Header.Get("Cache-Control") != "no-store" {
			b.h.t.Errorf("%s lacks no-store", path)
		}
	}
	return res, raw
}
func (b *browser) status(want bool) api.SessionResponse {
	b.h.t.Helper()
	res, raw := b.request("GET", api.PathAuthSession, "", nil)
	requireStatus(b.h.t, res, raw, 200)
	var s api.SessionResponse
	if err := json.Unmarshal(raw, &s); err != nil {
		b.h.t.Fatal(err)
	}
	if s.Authenticated != want || s.CSRFToken == "" {
		b.h.t.Fatalf("session = %+v, want authenticated %v and CSRF", s, want)
	}
	b.token = s.CSRFToken
	return s
}
func (b *browser) login(password string) (*http.Response, []byte) {
	raw, _ := json.Marshal(api.PasswordRequest{Password: password})
	res, body := b.request("POST", api.PathAuthLogin, string(raw), nil)
	if res.StatusCode == 200 {
		var s api.SessionResponse
		if err := json.Unmarshal(body, &s); err != nil {
			b.h.t.Fatal(err)
		}
		b.token = s.CSRFToken
	}
	return res, body
}
func requireStatus(t *testing.T, res *http.Response, raw []byte, want int) {
	t.Helper()
	if res.StatusCode != want {
		t.Fatalf("status=%d want=%d body=%s", res.StatusCode, want, raw)
	}
}

func TestPasswordAttemptsShareRecoverableGlobalWindow(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200) // success consumes one
	for i := 1; i < 10; i++ {
		path := api.PathAuthLogin
		if i%2 == 0 {
			path = api.PathAuthLogoutAll
		}
		res, raw = b.request("POST", path, `{"password":"wrong"}`, func(r *http.Request) { r.Header.Set("X-Forwarded-For", strings.Repeat("1", i)) })
		requireStatus(t, res, raw, 401)
	}
	// A different browser and forged forwarding metadata cannot split the budget.
	other := h.browser()
	other.status(false)
	res, raw = other.login(loginPassword)
	requireStatus(t, res, raw, 429)
	if res.Header.Get("Retry-After") != "300" {
		t.Fatalf("Retry-After=%q", res.Header.Get("Retry-After"))
	}
	h.clock.advance(5*time.Minute - time.Nanosecond)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 429)
	if res.Header.Get("Retry-After") != "1" {
		t.Fatalf("Retry-After=%q", res.Header.Get("Retry-After"))
	}
	h.clock.advance(time.Nanosecond)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
}

func TestSessionsAndRevocationsSurviveRestart(t *testing.T) {
	h := newAuthHarness(t)
	a, b := h.browser(), h.browser()
	a.status(false)
	res, raw := a.login(loginPassword)
	requireStatus(t, res, raw, 200)
	b.status(false)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	// A normal restart retains both independent browser credentials.
	h.stop()
	h.start()
	a.status(true)
	b.status(true)
	res, raw = a.request("POST", api.PathAuthLogout, "", nil)
	requireStatus(t, res, raw, 204)
	h.stop()
	h.start()
	a.status(false)
	b.status(true)
	// Wrong re-verification leaves every existing session unchanged.
	res, raw = b.request("POST", api.PathAuthLogoutAll, `{"password":"wrong"}`, nil)
	requireStatus(t, res, raw, 401)
	b.status(true)
	a.status(false)
	res, raw = a.login(loginPassword)
	requireStatus(t, res, raw, 200)
	body, _ := json.Marshal(api.PasswordRequest{Password: loginPassword})
	res, raw = b.request("POST", api.PathAuthLogoutAll, string(body), nil)
	requireStatus(t, res, raw, 204)
	h.stop()
	h.start()
	a.status(false)
	b.status(false)
}

func TestPasswordLoginProtectsBusinessBeforeParsing(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	res, raw := b.request("POST", "/messages", "not JSON", func(r *http.Request) { r.Header.Set("Authorization", "Bearer old-secret") })
	requireStatus(t, res, raw, 401)
	if h.judge.calls != 0 || h.memory.appendCalls != 0 {
		t.Fatal("unauthenticated request reached business")
	}
	b.status(false)
	for _, password := range []string{"", "wrong", strings.TrimSpace(loginPassword), strings.Repeat("字", 25)} {
		res, raw = b.login(password)
		requireStatus(t, res, raw, 401)
	}
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == auth.SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge <= 0 || cookie.Expires.IsZero() {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	if bytes.Contains(raw, []byte(cookie.Value)) {
		t.Fatal("credential leaked in JSON")
	}
	b.status(true)
	res, raw = b.request("POST", "/messages", `{"content":"原文不能因登录改写"}`, nil)
	requireStatus(t, res, raw, 201)
	if h.judge.content != "原文不能因登录改写" || h.memory.appendCalls != 1 {
		t.Fatal("authenticated record did not reach business unchanged")
	}
}
