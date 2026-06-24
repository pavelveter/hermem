package store

import (
	"encoding/binary"
	"math"
	"testing"
)

// TestBytesToFloat32Safe_HappyPath verifies a normal blob round-trips.
func TestBytesToFloat32Safe_HappyPath(t *testing.T) {
	want := []float32{1.0, -2.5, 3.14159, 0, 100, -1e6}
	buf := make([]byte, len(want)*4)
	for i, v := range want {
		binary.LittleEndian.PutUint32(buf[i*4:i*4+4], math.Float32bits(v))
	}
	got, err := BytesToFloat32Safe(buf)
	if err != nil {
		t.Fatalf("BytesToFloat32Safe: %v", err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx %d: want %v, got %v", i, want[i], got[i])
		}
	}
}

// TestBytesToFloat32Safe_RejectsNaN ensures NaN-bits in the blob surface
// as a decoding error rather than poisoning downstream matrix math.
func TestBytesToFloat32Safe_RejectsNaN(t *testing.T) {
	good := []float32{1, 2, 3}
	buf := make([]byte, len(good)*4)
	for i, v := range good {
		binary.LittleEndian.PutUint32(buf[i*4:i*4+4], math.Float32bits(v))
	}
	// Append a NaN bit pattern: 0x7fc00000 (IEEE-754 quiet NaN).
	buf = append(buf, 0x00, 0x00, 0xc0, 0x7f)
	if _, err := BytesToFloat32Safe(buf); err == nil {
		t.Fatal("expected NaN rejection, got nil")
	}
}

// TestBytesToFloat32Safe_RejectsInf covers +Inf and -Inf bit patterns.
func TestBytesToFloat32Safe_RejectsInf(t *testing.T) {
	cases := []struct {
		name string
		bits [4]byte
	}{
		{"posinf", [4]byte{0x00, 0x00, 0x80, 0x7f}},
		{"neginf", [4]byte{0x00, 0x00, 0x80, 0xff}},
	}
	for _, c := range cases {
		buf := c.bits[:]
		if _, err := BytesToFloat32Safe(buf); err == nil {
			t.Fatalf("%s: expected Inf rejection, got nil", c.name)
		}
	}
}

// TestBytesToFloat32Safe_RejectsBadLength ensures we error on a length
// drift rather than silently truncate.
func TestBytesToFloat32Safe_RejectsBadLength(t *testing.T) {
	if _, err := BytesToFloat32Safe([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected length error for non-multiple-of-4 blob")
	}
}
