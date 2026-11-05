package config

// AuthConfig is the frozen Web authentication configuration. The loader and
// validation are implemented in the backend ticket; declaring this type does
// not enable authentication or alter the legacy configuration path.
//
// Normal mode requires a bcrypt PasswordHash and one explicit HTTPS PublicOrigin.
// DevAnonymous must be explicitly enabled; missing/invalid credentials never
// select it. It skips password/session enforcement, not Origin or CSRF checks.
// PasswordHash must never be included in logs or validation errors.
type AuthConfig struct {
	PasswordHash string `toml:"password_hash"`
	StateDir     string `toml:"state_dir"`
	PublicOrigin string `toml:"public_origin"`
	DevAnonymous bool   `toml:"dev_anonymous"`
}

// Environment overrides win over TOML, including an explicitly empty value
// (which must fail validation rather than silently retain a file credential).
const (
	EnvAuthPasswordHash = "SHIYI_AUTH_PASSWORD_HASH"
	EnvAuthStateDir     = "SHIYI_AUTH_STATE_DIR"
	EnvAuthPublicOrigin = "SHIYI_AUTH_PUBLIC_ORIGIN"
	EnvDevAnonymous     = "SHIYI_DEV_ANONYMOUS"
	LegacyAuthTokenEnv  = "SHIYI_AUTH_TOKEN"
)

// These defaults are applied by the backend loader, not by this contract file.
// Empty hash/origin are deliberate fail-closed defaults in normal mode.
const (
	DefaultAuthStateDir     = "~/.local/state/shiyi/auth"
	DefaultAuthPasswordHash = ""
	DefaultAuthPublicOrigin = ""
	DefaultDevAnonymous     = false
)
