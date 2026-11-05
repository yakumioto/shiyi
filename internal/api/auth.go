package api

// Authentication routes are mounted without a path prefix or a trailing slash.
// Only the session endpoint accepts GET; all changes require POST.
const (
	PathAuthSession   = "/auth/session"
	PathAuthLogin     = "/auth/login"
	PathAuthLogout    = "/auth/logout"
	PathAuthLogoutAll = "/auth/logout-all"
	HeaderCSRFToken   = "X-CSRF-Token"
)

// Authentication modes are explicit; missing credentials never select development mode.
const (
	AuthModePassword             = "password"
	AuthModeDevelopmentAnonymous = "development_anonymous"
)

// SessionResponse is returned with 200 by GET /auth/session and a successful
// POST /auth/login. An unauthenticated session check also returns 200, with
// authenticated=false and a bootstrap CSRF token. A token is never a login
// credential and must be kept only in page memory. Session checks do not renew
// the idle deadline. Development mode reports authenticated=true explicitly.
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Mode          string `json:"mode"`
	CSRFToken     string `json:"csrf_token"`
}

// PasswordRequest is the JSON body of POST /auth/login and POST /auth/logout-all.
// Password is used verbatim, never trimmed, logged, placed in a URL or persisted.
// Empty passwords and passwords exceeding 72 UTF-8 bytes cannot authenticate.
type PasswordRequest struct {
	Password string `json:"password"`
}

// POST /auth/logout takes an empty body; successful logout and logout-all return
// 204 without a response body. All authentication and protected responses,
// including failures, carry Cache-Control: no-store.

// Authentication failures retain ErrorResponse's existing wire shape.
const (
	CodeForbidden       = "forbidden"
	CodeRateLimited     = "rate_limited"
	CodeAuthUnavailable = "auth_unavailable"
)

// Missing/expired/revoked/forged sessions and incorrect passwords all use the
// existing CodeUnauthorized. A 429 also carries Retry-After in whole seconds.
// Messages are fixed public text, never underlying errors or submitted secrets.
