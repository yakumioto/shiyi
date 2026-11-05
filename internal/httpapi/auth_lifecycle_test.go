package httpapi_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shiyi/internal/api"
	"shiyi/internal/auth"
	"shiyi/internal/config"
)

func TestIdleDeadlineIsNotExtendedByPollingOrRejectedRequests(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	h.clock.advance(6 * 24 * time.Hour)
	b.status(true)
	res, raw = b.request("GET", "/healthz", "", nil)
	requireStatus(t, res, raw, 200)
	res, raw = b.request("POST", "/messages", `{"content":"rejected"}`, func(r *http.Request) { r.Header.Del(api.HeaderCSRFToken) })
	requireStatus(t, res, raw, 403)
	h.clock.advance(24*time.Hour - time.Nanosecond)
	b.status(true)
	h.clock.advance(time.Nanosecond)
	res, raw = b.request("POST", "/messages", `{"content":"expired"}`, nil)
	requireStatus(t, res, raw, 401)
	h.stop()
	h.start()
	b.status(false)
	if h.judge.calls != 0 {
		t.Fatal("rejected request reached classifier")
	}
}

func TestAdmittedBusinessAttemptRefreshesDurablyBeforeDecoding(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	h.clock.advance(6 * 24 * time.Hour)
	res, raw = b.request("POST", "/messages", "not JSON", nil)
	requireStatus(t, res, raw, 400)
	h.stop()
	h.start()
	h.clock.advance(6 * 24 * time.Hour)
	b.status(true)
	h.clock.advance(24 * time.Hour)
	res, raw = b.request("POST", "/messages", `{"content":"idle"}`, nil)
	requireStatus(t, res, raw, 401)
}

func TestActiveSessionStillExpiresAtThirtyDays(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	for _, cookie := range res.Cookies() {
		if cookie.Name == auth.SessionCookieName && !cookie.Expires.Equal(time.Date(2030, 1, 31, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("absolute Cookie expiry=%v", cookie.Expires)
		}
	}
	for i := 0; i < 4; i++ {
		h.clock.advance(6 * 24 * time.Hour)
		res, raw = b.request("POST", "/messages", `{"content":"active"}`, nil)
		requireStatus(t, res, raw, 201)
	}
	h.clock.advance(6*24*time.Hour - time.Nanosecond)
	b.status(true)
	h.clock.advance(time.Nanosecond)
	res, raw = b.request("POST", "/messages", `{"content":"absolute expiry"}`, nil)
	requireStatus(t, res, raw, 401)
}

func TestReloginRotatesCredentialsAndRejectsCopiedRevokedCookie(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	oldToken := b.token
	var oldCookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == auth.SessionCookieName {
			oldCookie = c
		}
	}
	res, raw = b.login("wrong")
	requireStatus(t, res, raw, 401)
	b.status(true)
	res, raw = b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	for _, c := range res.Cookies() {
		if c.Name == auth.SessionCookieName && c.Value == oldCookie.Value {
			t.Fatal("login reused supplied credential")
		}
	}
	replay := &browser{h: h, client: h.server.Client(), token: oldToken}
	res, raw = replay.request("POST", "/messages", `{"content":"replay"}`, func(r *http.Request) { r.AddCookie(oldCookie) })
	requireStatus(t, res, raw, 401)
	res, raw = b.request("POST", api.PathAuthLogout, "", nil)
	requireStatus(t, res, raw, 204)
	for _, c := range res.Cookies() {
		if c.Name == auth.SessionCookieName && (c.MaxAge != -1 || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || !c.Expires.Before(h.clock.Now())) {
			t.Fatal("unsafe clearing cookie")
		}
	}
	h.stop()
	h.start()
	res, raw = replay.request("POST", "/messages", `{"content":"replay after restart"}`, func(r *http.Request) { r.AddCookie(oldCookie) })
	requireStatus(t, res, raw, 401)
}

func TestChangedHashAndLostStateRequireFreshLogin(t *testing.T) {
	for _, change := range []string{"hash", "lost state"} {
		t.Run(change, func(t *testing.T) {
			h := newAuthHarness(t)
			b := h.browser()
			b.status(false)
			res, raw := b.login(loginPassword)
			requireStatus(t, res, raw, 200)
			h.stop()
			if change == "hash" {
				hash, err := bcrypt.GenerateFromPassword([]byte(loginPassword), 10)
				if err != nil {
					t.Fatal(err)
				}
				h.cfg.PasswordHash = string(hash)
			} else {
				if err := os.Rename(h.cfg.StateDir, h.cfg.StateDir+"-lost"); err != nil {
					t.Fatal(err)
				}
			}
			h.start()
			res, raw = b.request("POST", "/messages", `{"content":"old credential"}`, nil)
			requireStatus(t, res, raw, 401)
			b.status(false)
			res, raw = b.login(loginPassword)
			requireStatus(t, res, raw, 200)
		})
	}
}

func TestStatePermissionsCorruptionAndSingleWriterFailClosed(t *testing.T) {
	h := newAuthHarness(t)
	if m, err := auth.New(h.cfg, auth.Options{Now: h.clock.Now}); !errors.Is(err, auth.ErrUnavailable) {
		if m != nil {
			m.Close()
		}
		t.Fatalf("second writer accepted: %v", err)
	}
	info, err := os.Stat(h.cfg.StateDir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("auth directory not private")
	}
	entries, err := os.ReadDir(h.cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("auth state not private regular files")
		}
	}
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	if err := h.app.Close(); err != nil {
		t.Fatal(err)
	}
	res, raw = b.request("POST", "/messages", `{"content":"closed"}`, nil)
	requireStatus(t, res, raw, 503)
	h.stop()
	h.start()
	b.status(true)
	h.stop()
	for _, mode := range []os.FileMode{0755, 0500} {
		if err := os.Chmod(h.cfg.StateDir, mode); err != nil {
			t.Fatal(err)
		}
		m, err := auth.New(h.cfg, auth.Options{})
		if m != nil {
			m.Close()
		}
		if !errors.Is(err, auth.ErrUnavailable) {
			t.Fatalf("unsafe state directory accepted: %v", err)
		}
	}
	if err := os.Chmod(h.cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Corrupt opaque state through the filesystem boundary, without asserting its format.
	for _, e := range entries {
		if err := os.WriteFile(filepath.Join(h.cfg.StateDir, e.Name()), []byte("corrupt-state-marker"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := auth.New(h.cfg, auth.Options{})
	if m != nil {
		m.Close()
	}
	if !errors.Is(err, auth.ErrUnavailable) || strings.Contains(err.Error(), "corrupt-state-marker") {
		t.Fatalf("corrupt state not rejected safely: %v", err)
	}
}

func TestUnavailableDurableStateCannotAuthorizeOrReportLogoutSuccess(t *testing.T) {
	for _, path := range []string{"/messages", api.PathAuthLogout, api.PathAuthLogoutAll, api.PathAuthLogin} {
		t.Run(path, func(t *testing.T) {
			h := newAuthHarness(t)
			b := h.browser()
			b.status(false)
			res, raw := b.login(loginPassword)
			requireStatus(t, res, raw, 200)
			if err := os.Chmod(h.cfg.StateDir, 0500); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(h.cfg.StateDir, 0700)
			res, raw = b.request("POST", path, `{"content":"not admitted","password":"ignored"}`, nil)
			requireStatus(t, res, raw, 503)
			if len(res.Cookies()) != 0 {
				t.Fatal("failed state mutation changed browser credentials")
			}
			if h.judge.calls != 0 || h.memory.appendCalls != 0 {
				t.Fatal("failed refresh admitted business")
			}
			if err := os.Chmod(h.cfg.StateDir, 0700); err != nil {
				t.Fatal(err)
			}
			h.stop()
			h.start()
			b.status(true)
		})
	}
}

func TestAuthConstructorRejectsBadHashesAndOrigins(t *testing.T) {
	for _, hash := range []string{"", " ", "plaintext-secret", strings.Replace(testPasswordHash(t), "$10$", "$09$", 1), strings.Replace(testPasswordHash(t), "$10$", "$15$", 1), testPasswordHash(t) + "x", "$2a$10$" + strings.Repeat("!", 53)} {
		cfg := config.AuthConfig{PasswordHash: hash, StateDir: filepath.Join(t.TempDir(), "auth"), PublicOrigin: "https://memory.example"}
		m, err := auth.New(cfg, auth.Options{})
		if m != nil {
			m.Close()
		}
		if !errors.Is(err, auth.ErrInvalidConfig) {
			t.Fatal("invalid password hash accepted")
		}
		if len(hash) > 4 && strings.Contains(err.Error(), hash) {
			t.Fatal("hash leaked in startup error")
		}
	}
	for _, origin := range []string{"", "http://memory.example", "https://memory.example/", "https://u:p@memory.example", "https://memory.example?x=y", "https://memory.example:65536"} {
		cfg := config.AuthConfig{PasswordHash: testPasswordHash(t), StateDir: filepath.Join(t.TempDir(), "auth"), PublicOrigin: origin}
		m, err := auth.New(cfg, auth.Options{})
		if m != nil {
			m.Close()
		}
		if !errors.Is(err, auth.ErrInvalidConfig) {
			t.Fatal("invalid origin accepted")
		}
	}
}
