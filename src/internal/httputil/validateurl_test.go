package httputil

import "testing"

func TestValidateURL_Valid(t *testing.T) {
	valid := []string{
		"http://example.com",
		"https://example.com/path?q=1",
		"https://host:8080/api",
	}
	for _, u := range valid {
		if err := validateURL(u); err != nil {
			t.Errorf("validateURL(%q) unexpected error: %v", u, err)
		}
	}
}

func TestValidateURL_Invalid(t *testing.T) {
	tests := []struct {
		url string
		err string
	}{
		{"", "empty host"},
		{"ftp://example.com", "invalid URL scheme"},
		{"/no-scheme", "empty host"},
		{"://malformed", "invalid URL"},
	}
	for _, tt := range tests {
		err := validateURL(tt.url)
		if err == nil {
			t.Errorf("validateURL(%q) expected error, got nil", tt.url)
		}
	}
}
