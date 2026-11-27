package domain

// Auth event types recorded in the authentication audit log (#100).
const (
	AuthLoginSuccess           = "login_success"
	AuthLoginFailure           = "login_failure"
	AuthLogout                 = "logout"
	AuthTokenUsed              = "token_used"
	AuthSessionRevoked         = "session_revoked"
	Auth2FASuccess             = "2fa_success"
	Auth2FAFailure             = "2fa_failure"
	AuthSSOLogin               = "sso_login"
	AuthRoleChanged            = "role_changed"
	AuthTokenCreated           = "token_created"
	AuthTokenRevoked           = "token_revoked"
	AuthTokenRotationStarted   = "token_rotation_started"
	AuthTokenRotationCompleted = "token_rotation_completed"
)

// AuthEvent is one recorded authentication-relevant event.
type AuthEvent struct {
	ID        int64
	Type      string
	Username  string
	UserID    int64
	SourceIP  string
	Detail    string
	CreatedAt string
}
