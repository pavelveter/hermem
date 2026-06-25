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

// requiredScopes is the immutable prefix→scope mapping used by ScopeForPath.
// Kept unexported so no external package can mutate the map at runtime; use
// RequiredScopesMap() for a safe read-only snapshot.
var requiredScopes = map[string]Scope{
	"ingest":   ScopeWrite,
	"search":   ScopeRead,
	"retrieve": ScopeRead,
	"query":    ScopeRead,
	"admin":    ScopeAdmin,
}

// RequiredScopesMap returns a copy of the prefix→scope mapping so callers
// can iterate without risking concurrent mutation of the canonical map.
func RequiredScopesMap() map[string]Scope {
	out := make(map[string]Scope, len(requiredScopes))
	for k, v := range requiredScopes {
		out[k] = v
	}
	return out
}

func ScopeForPath(path string) Scope {
	for prefix, scope := range requiredScopes {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return scope
		}
	}
	return ScopeWrite
}
