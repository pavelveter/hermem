package auth

type Scope string

const (
	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"
	ScopeAdmin Scope = "admin"
)

func ParseScope(s string) Scope {
	switch s {
	case "read":
		return ScopeRead
	case "write":
		return ScopeWrite
	case "admin":
		return ScopeAdmin
	default:
		return Scope("")
	}
}

type Key struct {
	Value string
	Scope Scope
	Label string
}

type Authenticator interface {
	Authorize(rawValue string, requiredScope Scope) (*Key, bool, error)
}
