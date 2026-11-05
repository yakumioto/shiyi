package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shiyi/internal/api"
	"shiyi/internal/config"
)

type loginSession struct {
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"`
	CSRF     string    `json:"csrf"`
}
type module struct {
	mu          sync.Mutex
	cfg         config.AuthConfig
	now         func() time.Time
	origin      string
	key         []byte
	sessions    map[string]loginSession
	windowStart time.Time
	attempts    int
	closed      bool
	failed      bool
	root        *os.Root
	lock        *os.File
}

var _ NewFunc = New
var hashEncoding = regexp.MustCompile(`^\$2[aby]\$(10|11|12|13|14)\$[./A-Za-z0-9]{53}$`)

// New constructs the HTTP authentication boundary. It never selects development
// mode implicitly and does not contact the model.
func New(cfg config.AuthConfig, opts Options) (Module, error) {
	origin, ok := canonicalOrigin(cfg.PublicOrigin)
	if !ok || (!cfg.DevAnonymous && !strings.HasPrefix(origin, "https://")) {
		return nil, ErrInvalidConfig
	}
	if !cfg.DevAnonymous {
		cost, err := bcrypt.Cost([]byte(cfg.PasswordHash))
		if err != nil || !hashEncoding.MatchString(cfg.PasswordHash) || cost < MinPasswordHashCost || cost > MaxPasswordHashCost || strings.TrimSpace(cfg.StateDir) == "" {
			return nil, ErrInvalidConfig
		}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, ErrUnavailable
	}
	m := &module{cfg: cfg, now: now, origin: origin, key: key, sessions: make(map[string]loginSession)}
	if !cfg.DevAnonymous {
		if err := m.openState(); err != nil {
			_ = m.closeState()
			return nil, err
		}
	}
	return m, nil
}
func (m *module) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeState()
}

func canonicalOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, " \t\r\n,#?\\") {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(u.Host, ":") {
		return "", false
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
		port = strconv.Itoa(n)
	}
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return u.Scheme + "://" + host + ":" + port, true
}
func (m *module) sourceOK(r *http.Request, required bool) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return !required
	}
	if len(values) != 1 {
		return false
	}
	origin, ok := canonicalOrigin(values[0])
	return ok && origin == m.origin
}
func jsonContent(r *http.Request) bool {
	v := r.Header.Values("Content-Type")
	if len(v) != 1 {
		return false
	}
	kind, _, err := mime.ParseMediaType(v[0])
	return err == nil && kind == "application/json"
}
func secret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(s string) string {
	v := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(v[:])
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func cookieValue(r *http.Request, name string) string {
	var value string
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == name {
			count++
			value = c.Value
		}
	}
	if count != 1 {
		return ""
	}
	return value
}
func (m *module) cookie(w http.ResponseWriter, name, value string, expires time.Time) {
	age := int(expires.Sub(m.now()).Seconds())
	if value == "" {
		age = -1
		expires = time.Unix(1, 0)
	} else if age < 1 {
		age = 1
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !m.cfg.DevAnonymous, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: age})
}
func (m *module) bootstrapName() string {
	if m.cfg.DevAnonymous {
		return DevBootstrapCookieName
	}
	return BootstrapCookieName
}
func (m *module) mac(value string) string {
	h := hmac.New(sha256.New, m.key)
	h.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (m *module) bootstrap(r *http.Request) (string, string, bool) {
	value := cookieValue(r, m.bootstrapName())
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !m.now().Before(time.Unix(expires, 0)) || !equal(parts[2], m.mac(parts[0]+"."+parts[1])) {
		return "", "", false
	}
	return value, m.mac("csrf:" + value), true
}
func (m *module) session(r *http.Request) (string, loginSession, bool) {
	id := digest(cookieValue(r, SessionCookieName))
	s, ok := m.sessions[id]
	if !ok || !m.now().Before(s.LastUsed.Add(SessionIdleTimeout)) || !m.now().Before(s.Created.Add(SessionAbsoluteTTL)) {
		return "", loginSession{}, false
	}
	return id, s, true
}
func (m *module) csrfOK(r *http.Request, s loginSession, logged bool) bool {
	values := r.Header.Values(api.HeaderCSRFToken)
	if len(values) != 1 || values[0] == "" {
		return false
	}
	token := s.CSRF
	if !logged {
		_, t, ok := m.bootstrap(r)
		if !ok {
			return false
		}
		token = t
	}
	return equal(values[0], token)
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code string) {
	messages := map[string]string{api.CodeUnauthorized: "需要有效登录", api.CodeForbidden: "来源或防伪校验失败", api.CodeInvalidRequest: "请求无效", api.CodeRequestTooLarge: "请求体超过上限", api.CodeAuthUnavailable: "认证暂时不可用", api.CodeRateLimited: "密码尝试过多，请稍后重试"}
	respond(w, api.HTTPStatusForCode(code), api.ErrorResponse{Error: api.ErrorBody{Code: code, Message: messages[code]}})
}
func (m *module) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.failed || !m.stateSafe() {
		fail(w, api.CodeAuthUnavailable)
		return
	}
	method := "POST"
	switch r.URL.Path {
	case api.PathAuthSession:
		method = "GET"
	case api.PathAuthLogin, api.PathAuthLogout, api.PathAuthLogoutAll:
	default:
		respond(w, 404, api.ErrorResponse{Error: api.ErrorBody{Code: api.CodeInvalidRequest, Message: "没有这个接口"}})
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		respond(w, 405, api.ErrorResponse{Error: api.ErrorBody{Code: api.CodeInvalidRequest, Message: "方法不允许"}})
		return
	}
	id, s, logged := m.session(r)
	if method == "GET" {
		if !m.sourceOK(r, false) {
			fail(w, api.CodeForbidden)
			return
		}
		if logged && !m.cfg.DevAnonymous {
			respond(w, 200, api.SessionResponse{Authenticated: true, Mode: api.AuthModePassword, CSRFToken: s.CSRF})
			return
		}
		if cookieValue(r, SessionCookieName) != "" && !m.cfg.DevAnonymous {
			m.cookie(w, SessionCookieName, "", time.Time{})
		}
		_, token, ok := m.bootstrap(r)
		if !ok {
			expires := m.now().Add(BootstrapCSRFTTL)
			value := secret() + "." + strconv.FormatInt(expires.Unix(), 10)
			value += "." + m.mac(value)
			token = m.mac("csrf:" + value)
			m.cookie(w, m.bootstrapName(), value, expires)
		}
		mode := api.AuthModePassword
		if m.cfg.DevAnonymous {
			mode = api.AuthModeDevelopmentAnonymous
		}
		respond(w, 200, api.SessionResponse{Authenticated: m.cfg.DevAnonymous, Mode: mode, CSRFToken: token})
		return
	}
	if r.URL.Path != api.PathAuthLogin && !logged && !m.cfg.DevAnonymous {
		fail(w, api.CodeUnauthorized)
		return
	}
	if !m.sourceOK(r, true) || !m.csrfOK(r, s, logged) {
		fail(w, api.CodeForbidden)
		return
	}
	if !jsonContent(r) {
		fail(w, api.CodeInvalidRequest)
		return
	}
	if m.cfg.DevAnonymous {
		if r.URL.Path != api.PathAuthLogout {
			fail(w, api.CodeInvalidRequest)
			return
		}
		m.cookie(w, m.bootstrapName(), "", time.Time{})
		w.WriteHeader(204)
		return
	}
	if r.URL.Path == api.PathAuthLogout {
		next := maps.Clone(m.sessions)
		delete(next, id)
		if err := m.commit(next); err != nil {
			fail(w, api.CodeAuthUnavailable)
			return
		}
		m.cookie(w, SessionCookieName, "", time.Time{})
		w.WriteHeader(204)
		return
	}
	// Never hold the global authorization lock while waiting for client bytes.
	// Revalidate after the read: another request may revoke or expire this session.
	m.mu.Unlock()
	body := http.MaxBytesReader(w, r.Body, MaxAuthBodyBytes)
	raw, err := io.ReadAll(body)
	m.mu.Lock()
	if m.closed || m.failed || !m.stateSafe() {
		fail(w, api.CodeAuthUnavailable)
		return
	}
	id, s, logged = m.session(r)
	if r.URL.Path == api.PathAuthLogoutAll && !logged {
		fail(w, api.CodeUnauthorized)
		return
	}
	if !m.sourceOK(r, true) || !m.csrfOK(r, s, logged) {
		fail(w, api.CodeForbidden)
		return
	}
	if err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			fail(w, api.CodeRequestTooLarge)
		} else {
			fail(w, api.CodeInvalidRequest)
		}
		return
	}
	var input api.PasswordRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF || json.Unmarshal(raw, &fields) != nil || fields == nil || bytes.Equal(bytes.TrimSpace(fields["password"]), []byte("null")) {
		fail(w, api.CodeInvalidRequest)
		return
	}

	now := m.now()
	if m.attempts == 0 || !now.Before(m.windowStart.Add(PasswordAttemptWindow)) {
		m.windowStart = now
		m.attempts = 0
	}
	if m.attempts >= PasswordAttemptLimit {
		remaining := m.windowStart.Add(PasswordAttemptWindow).Sub(now)
		seconds := int64((remaining + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		fail(w, api.CodeRateLimited)
		return
	}
	m.attempts++
	if input.Password == "" || len(input.Password) > MaxPasswordBytes {
		fail(w, api.CodeUnauthorized)
		return
	}
	// Attempts were reserved atomically above; expensive comparisons do not block
	// session checks or revocation. At most ten can start in one fixed window.
	m.mu.Unlock()
	passwordErr := bcrypt.CompareHashAndPassword([]byte(m.cfg.PasswordHash), []byte(input.Password))
	m.mu.Lock()
	if m.closed || m.failed || !m.stateSafe() {
		fail(w, api.CodeAuthUnavailable)
		return
	}
	id, s, logged = m.session(r)
	if r.URL.Path == api.PathAuthLogoutAll && !logged {
		fail(w, api.CodeUnauthorized)
		return
	}
	if !m.sourceOK(r, true) || !m.csrfOK(r, s, logged) {
		fail(w, api.CodeForbidden)
		return
	}
	if passwordErr != nil {
		fail(w, api.CodeUnauthorized)
		return
	}
	if r.URL.Path == api.PathAuthLogoutAll {
		if err := m.commit(make(map[string]loginSession)); err != nil {
			fail(w, api.CodeAuthUnavailable)
			return
		}
		m.cookie(w, SessionCookieName, "", time.Time{})
		w.WriteHeader(204)
		return
	}
	credential := secret()
	s = loginSession{Created: m.now(), LastUsed: m.now(), CSRF: secret()}
	next := maps.Clone(m.sessions)
	if logged {
		delete(next, id)
	}
	next[digest(credential)] = s
	if err := m.commit(next); err != nil {
		fail(w, api.CodeAuthUnavailable)
		return
	}
	m.cookie(w, SessionCookieName, credential, s.Created.Add(SessionAbsoluteTTL))
	m.cookie(w, BootstrapCookieName, "", time.Time{})
	respond(w, 200, api.SessionResponse{Authenticated: true, Mode: api.AuthModePassword, CSRFToken: s.CSRF})
}
func (m *module) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		m.mu.Lock()
		if m.closed || m.failed || !m.stateSafe() {
			m.mu.Unlock()
			fail(w, api.CodeAuthUnavailable)
			return
		}
		id, s, logged := m.session(r)
		if !logged && !m.cfg.DevAnonymous {
			m.mu.Unlock()
			fail(w, api.CodeUnauthorized)
			return
		}
		if !m.sourceOK(r, true) || !m.csrfOK(r, s, logged) {
			m.mu.Unlock()
			fail(w, api.CodeForbidden)
			return
		}
		if !jsonContent(r) {
			m.mu.Unlock()
			fail(w, api.CodeInvalidRequest)
			return
		}
		if logged {
			s.LastUsed = m.now()
			next := maps.Clone(m.sessions)
			next[id] = s
			if err := m.commit(next); err != nil {
				m.mu.Unlock()
				fail(w, api.CodeAuthUnavailable)
				return
			}
		}
		m.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}
