package core

// NormalizeSlice returns s if non-nil, otherwise an empty slice of the same type.
// Eliminates the repetitive nil→empty guard pattern found in ~20+ service methods:
//
//	if out == nil { out = []T{} }
//	return out
//
// Becomes:
//
//	return core.NormalizeSlice(out)
func NormalizeSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
