package auth

func (s Scope) CanAccess(required Scope) bool {
	if s == ScopeAdmin {
		return true
	}
	if s == ScopeWrite && required != ScopeAdmin {
		return true
	}
	if s == ScopeRead && required == ScopeRead {
		return true
	}
	return false
}

var RequiredScopes = map[string]Scope{
	"ingest":   ScopeWrite,
	"search":   ScopeRead,
	"retrieve": ScopeRead,
	"query":    ScopeRead,
	"admin":    ScopeAdmin,
}

func ScopeForPath(path string) Scope {
	for prefix, scope := range RequiredScopes {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return scope
		}
	}
	return ScopeWrite
}
