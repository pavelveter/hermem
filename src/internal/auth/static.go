package auth

import (
	"crypto/subtle"
	"errors"
)

var (
	ErrInvalidKey        = errors.New("auth: invalid api key")
	ErrInsufficientScope = errors.New("auth: insufficient scope")
)

type StaticAuthenticator struct {
	keys []Key
}

func NewStaticAuthenticator(keys []Key) *StaticAuthenticator {
	return &StaticAuthenticator{keys: keys}
}

func (s *StaticAuthenticator) Authorize(raw string, required Scope) (*Key, bool, error) {
	if raw == "" {
		return nil, false, ErrInvalidKey
	}
	rawBytes := []byte(raw)
	for i := range s.keys {
		keyBytes := []byte(s.keys[i].Value)
		if len(rawBytes) != len(keyBytes) {
			continue
		}
		if subtle.ConstantTimeCompare(rawBytes, keyBytes) == 1 {
			if !s.keys[i].Scope.CanAccess(required) {
				return nil, false, ErrInsufficientScope
			}
			return &s.keys[i], true, nil
		}
	}
	return nil, false, ErrInvalidKey
}
