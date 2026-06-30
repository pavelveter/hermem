package evidence

import (
	"encoding/json"
	"testing"
)

func TestPolarity_UnmarshalText_Valid(t *testing.T) {
	for _, v := range []Polarity{PolaritySupport, PolarityRefute} {
		var p Polarity
		if err := p.UnmarshalText([]byte(v)); err != nil {
			t.Errorf("UnmarshalText(%q): unexpected error: %v", v, err)
		}
		if p != v {
			t.Errorf("UnmarshalText(%q): got %q", v, p)
		}
	}
}

func TestPolarity_UnmarshalText_Invalid(t *testing.T) {
	var p Polarity
	if err := p.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatal("UnmarshalText(bogus): expected error")
	}
}

func TestPolarity_UnmarshalJSON_RoundTrip(t *testing.T) {
	for _, orig := range []Polarity{PolaritySupport, PolarityRefute} {
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", orig, err)
		}
		var got Polarity
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", b, err)
		}
		if got != orig {
			t.Errorf("round-trip: want %q, got %q", orig, got)
		}
	}
}

func TestPolarity_UnmarshalJSON_Invalid(t *testing.T) {
	var p Polarity
	if err := json.Unmarshal([]byte(`"bogus"`), &p); err == nil {
		t.Fatal("UnmarshalJSON(bogus): expected error")
	}
}
