// Package auth owns Web authentication: password verification, login sessions,
// durable revocation, rate limiting, cookies, trusted origin and CSRF checks.
// This file freezes its interface only; it deliberately supplies no permissive
// implementation and no New function. The backend ticket implements New.
package auth

import (
	"errors"
	"net/http"
	"time"

	"shiyi/internal/config"
)

// Module is the deep module mounted by httpapi. ServeHTTP serves only the four
// api.PathAuth* endpoints. Protect is applied to POST /messages before the body
// is read, classified, searched or stored. Neither operation depends on a model.
// Implementations must be safe for concurrent requests.
//
// Protect rejects an invalid login session with 401 before parsing the body;
// a valid session additionally requires the configured Origin, a matching CSRF
// token and application/json. Only a successfully admitted protected business
// request advances idle time (even if the downstream model later fails). Health,
// assets, session checks and rejected requests never advance it. A failed durable
// refresh returns 503 without invoking next. Expiry is checked before refresh.
//
// In explicit development-anonymous mode only the password/session requirement
// is skipped; configured Origin and browser-bound CSRF remain mandatory.
// All responses, including failures, must carry Cache-Control: no-store.
type Module interface {
	http.Handler
	Protect(next http.Handler) http.Handler
	// Close releases the state-directory lock after HTTP serving has stopped.
	// It is idempotent; a closed module rejects requests rather than authorizing.
	Close() error
}

// Options contains the single variable dependency needed by HTTP time tests.
// A nil Now means time.Now. The same clock controls idle/absolute expiration,
// bootstrap CSRF expiration, password-attempt limits, Cookie Expires and Retry-After.
// Tests supply a concurrency-safe clock and never sleep to advance auth time.
type Options struct {
	Now func() time.Time
}

// NewFunc freezes the future constructor:
//
//	func New(cfg config.AuthConfig, opts Options) (Module, error)
//
// New validates configuration and safely loads/creates independent auth state.
// Missing state starts a new empty store; corrupt, insecure or unreadable state
// fails closed. Every mutation is durable before success; changed PasswordHash
// invalidates all prior sessions, even when it encodes the same password.
// A second process using the same state directory must fail, not race writes.
// There is intentionally no constructor variable or placeholder authorization.
type NewFunc func(cfg config.AuthConfig, opts Options) (Module, error)

// Public error categories are matched with errors.Is, never message strings.
// HTTP handlers translate them using the frozen mapping in seams.md; constructor
// failures return ErrInvalidConfig or ErrUnavailable, without echoing secrets.
var (
	ErrInvalidConfig   = errors.New("auth: invalid configuration")
	ErrUnauthenticated = errors.New("auth: authentication required")
	ErrForbidden       = errors.New("auth: origin or CSRF rejected")
	ErrRateLimited     = errors.New("auth: password attempt limit exceeded")
	ErrUnavailable     = errors.New("auth: state unavailable")
)

// Fixed security policy, not end-user configuration knobs. bcrypt from
// golang.org/x/crypto/bcrypt is the selected password implementation; the backend
// ticket adds/pins/vendors it. Values are bytes, not Unicode character counts.
const (
	PasswordHashAlgorithm = "bcrypt"
	PasswordHashCost      = 12
	MinPasswordHashCost   = 10
	MaxPasswordHashCost   = 14
	MaxPasswordBytes      = 72
	MaxAuthBodyBytes      = 4096
	SessionIdleTimeout    = 7 * 24 * time.Hour
	SessionAbsoluteTTL    = 30 * 24 * time.Hour
	BootstrapCSRFTTL      = 20 * time.Minute
	PasswordAttemptLimit  = 10
	PasswordAttemptWindow = 5 * time.Minute
)

// Production cookies are host-only (no Domain), Path=/, HttpOnly, Secure and
// SameSite=Strict. Session cookies persist no longer than the absolute deadline;
// the server still enforces idle expiry. Bootstrap cookies expire after 20m.
// Development names permit HTTP only in explicit development-anonymous mode;
// they never confer a password-authenticated session.
const (
	SessionCookieName      = "__Host-shiyi-session"
	BootstrapCookieName    = "__Host-shiyi-csrf"
	DevBootstrapCookieName = "shiyi-dev-csrf"
)
