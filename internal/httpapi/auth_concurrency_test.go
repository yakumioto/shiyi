package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"shiyi/internal/api"
)

type observedBody struct {
	io.ReadCloser
	once    sync.Once
	reading chan struct{}
}

func (b *observedBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.reading) })
	return b.ReadCloser.Read(p)
}

func TestSlowPasswordBodyDoesNotBlockSessionChecksOrRevocation(t *testing.T) {
	h := newAuthHarness(t)
	b := h.browser()
	b.status(false)
	res, raw := b.login(loginPassword)
	requireStatus(t, res, raw, 200)
	h.stop()
	reading := make(chan struct{})
	h.wrap = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == api.PathAuthLogin {
				r.Body = &observedBody{ReadCloser: r.Body, reading: reading}
			}
			next.ServeHTTP(w, r)
		})
	}
	h.start()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	req, err := http.NewRequestWithContext(ctx, "POST", h.server.URL+api.PathAuthLogin, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", h.cfg.PublicOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.HeaderCSRFToken, b.token)
	done := make(chan error, 1)
	status := make(chan int, 1)
	go func() {
		resp, err := b.client.Do(req)
		if err == nil {
			status <- resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-reading:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not begin the streamed password request")
	}
	// The auth clock is not advanced; this deadline only detects a blocked server.
	probe := *b.client
	probe.Timeout = time.Second
	result, err := probe.Get(h.server.URL + api.PathAuthSession)
	if err != nil {
		t.Fatalf("slow password body blocked session check: %v", err)
	}
	result.Body.Close()
	if result.StatusCode != 200 {
		t.Fatalf("session check status=%d", result.StatusCode)
	}
	res, raw = b.request("POST", api.PathAuthLogout, "", nil)
	requireStatus(t, res, raw, 204)
	payload, _ := json.Marshal(api.PasswordRequest{Password: loginPassword})
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("password request did not finish")
	}
	if got := <-status; got != 403 {
		t.Fatalf("revoked request credential resurrected login: status=%d", got)
	}
	b.status(false)
}
