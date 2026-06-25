package vector

import (
	"math"
	"testing"
)

// QuantizeVector: round-trips within a small tolerance band (int8 precision loss).
func TestQuantizeVector_RoundtripApprox(t *testing.T) {
	src := []float32{-1.0, -0.5, 0, 0.5, 1.0}
	qv := QuantizeVector(src)
	if len(qv.Codes) != len(src) {
		t.Fatalf("code length: want %d, got %d", len(src), len(qv.Codes))
	}
	if qv.Min != -1 || qv.Max != 1 {
		t.Fatalf("min/max: want -1/1, got %v/%v", qv.Min, qv.Max)
	}
	recon := DequantizeVector(qv)
	max := float32(0)
	for i := range src {
		diff := src[i] - recon[i]
		if math.Abs(float64(diff)) > float64(max) {
			max = diff
		}
	}
	// Max single-step quant error: (max-min)/127 ≈ 2/127 ≈ 0.016
	tol := float32(2.0 / 127.0)
	if math.Abs(float64(max)) > float64(tol)+float64(eps) {
		t.Fatalf("reconstruction error %v exceeds tol %v", max, tol)
	}
}

func TestQuantizeVector_AllSameValue(t *testing.T) {
	src := []float32{7, 7, 7}
	qv := QuantizeVector(src)
	if qv.Min != 7 || qv.Max != 7 {
		t.Fatalf("min/max should be 7/7, got %v/%v", qv.Min, qv.Max)
	}
	for i, c := range qv.Codes {
		if c != 0 {
			t.Fatalf("flat signal: code %d should be 0, got %d", i, c)
		}
	}
}

func TestQuantizeVector_Empty(t *testing.T) {
	qv := QuantizeVector(nil)
	if qv.Min != 0 || qv.Max != 0 || len(qv.Codes) != 0 {
		t.Fatalf("empty should yield zero QuantizedVector, got %+v", qv)
	}
}

func TestQuantizeVector_CodesClampRange(t *testing.T) {
	// After min-max scaling by 127, codes must land in [-127, 127] (max int8 is 127, not 128).
	src := []float32{0, 100}
	qv := QuantizeVector(src)
	for i, c := range qv.Codes {
		if c < -127 {
			t.Fatalf("code %d out of int8 range: %d", i, c)
		}
	}
}

// QuantizedToBytes / BytesToQuantized round-trip
func TestQuantizedToBytes_Roundtrip(t *testing.T) {
	qv := QuantizedVector{
		Min:   -0.42,
		Max:   0.42,
		Codes: []int8{-100, -1, 0, 1, 100, 127, -127},
	}
	blob := QuantizedToBytes(qv)
	if len(blob) != 8+len(qv.Codes) {
		t.Fatalf("blob size: want %d, got %d", 8+len(qv.Codes), len(blob))
	}
	got, err := BytesToQuantized(blob)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Min != qv.Min || got.Max != qv.Max {
		t.Fatalf("min/max: want %v/%v, got %v/%v", qv.Min, qv.Max, got.Min, got.Max)
	}
	for i := range qv.Codes {
		if got.Codes[i] != qv.Codes[i] {
			t.Fatalf("code %d: want %d, got %d", i, qv.Codes[i], got.Codes[i])
		}
	}
}

func TestBytesToQuantized_TooShort(t *testing.T) {
	if _, err := BytesToQuantized([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error on short blob")
	}
}

func TestBytesToQuantized_EmptyCodes(t *testing.T) {
	// 8-byte header, no codes — legal.
	qv, err := BytesToQuantized(make([]byte, 8))
	if err != nil {
		t.Fatalf("8-byte header should be legal: %v", err)
	}
	if len(qv.Codes) != 0 {
		t.Fatalf("codes: want 0, got %d", len(qv.Codes))
	}
}

// DequantizeVector handles max == min (flat signal)
func TestDequantizeVector_FlatSignal(t *testing.T) {
	qv := QuantizedVector{Min: 3, Max: 3, Codes: []int8{0, 0, 0}}
	v := DequantizeVector(qv)
	for i, x := range v {
		if x != 3 {
			t.Fatalf("flat dequant: index %d want 3 got %v", i, x)
		}
	}
}
