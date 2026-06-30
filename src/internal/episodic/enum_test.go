package episodic

import (
	"encoding/json"
	"testing"
)

func TestEventType_UnmarshalText_Valid(t *testing.T) {
	valid := []EventType{EventMessage, EventAction, EventObservation, EventSystem}
	for _, v := range valid {
		var et EventType
		if err := et.UnmarshalText([]byte(v)); err != nil {
			t.Errorf("UnmarshalText(%q): unexpected error: %v", v, err)
		}
		if et != v {
			t.Errorf("UnmarshalText(%q): got %q", v, et)
		}
	}
}

func TestEventType_UnmarshalText_Invalid(t *testing.T) {
	var et EventType
	if err := et.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatal("UnmarshalText(bogus): expected error")
	}
}

func TestEventType_UnmarshalJSON_RoundTrip(t *testing.T) {
	valid := []EventType{EventMessage, EventAction, EventObservation, EventSystem}
	for _, orig := range valid {
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", orig, err)
		}
		var got EventType
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", b, err)
		}
		if got != orig {
			t.Errorf("round-trip: want %q, got %q", orig, got)
		}
	}
}

func TestTimelineEntryKind_UnmarshalText_Valid(t *testing.T) {
	valid := []TimelineEntryKind{TimelineEvent, TimelineMemory, TimelineTask}
	for _, v := range valid {
		var k TimelineEntryKind
		if err := k.UnmarshalText([]byte(v)); err != nil {
			t.Errorf("UnmarshalText(%q): unexpected error: %v", v, err)
		}
		if k != v {
			t.Errorf("UnmarshalText(%q): got %q", v, k)
		}
	}
}

func TestTimelineEntryKind_UnmarshalText_Invalid(t *testing.T) {
	var k TimelineEntryKind
	if err := k.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatal("UnmarshalText(bogus): expected error")
	}
}

func TestTimelineEntryKind_UnmarshalJSON_RoundTrip(t *testing.T) {
	valid := []TimelineEntryKind{TimelineEvent, TimelineMemory, TimelineTask}
	for _, orig := range valid {
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", orig, err)
		}
		var got TimelineEntryKind
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", b, err)
		}
		if got != orig {
			t.Errorf("round-trip: want %q, got %q", orig, got)
		}
	}
}
