package vector

import (
	"encoding/binary"
	"fmt"
	"math"
)

// QuantizedVector is an INT8 min-max quantized vector with reconstruction bounds.
type QuantizedVector struct {
	Min   float32 `json:"min"`
	Max   float32 `json:"max"`
	Codes []int8  `json:"codes"`
}

// QuantizeVector quantizes a float32 vector to int8 using the min-max scheme.
//
//	bytes per vector: 8 (min/max) + dim (codes) — about 4x smaller than float32.
//
// NaN/Inf values in the input are clamped to finite bounds to prevent
// garbage quantization codes.
func QuantizeVector(v []float32) QuantizedVector {
	if len(v) == 0 {
		return QuantizedVector{}
	}
	min, max := v[0], v[0]
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			continue
		}
		if x < min {
			min = x
		}
		if x > max {
			max = x
		}
	}
	// All NaN/Inf — return zero codes.
	if math.IsNaN(float64(min)) || math.IsInf(float64(min), 0) {
		return QuantizedVector{Min: 0, Max: 0, Codes: make([]int8, len(v))}
	}
	scale := float32(127) / (max - min)
	if max == min {
		scale = 1
	}
	if math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		scale = 1
	}
	// Codes must be an owned slice: callers keep the QuantizedVector beyond
	// the call, so returning pooled memory would alias later quantizations.
	codes := make([]int8, len(v))
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			codes[i] = 0
			continue
		}
		val := (x - min) * scale
		if val < -128 {
			val = -128
		} else if val > 127 {
			val = 127
		}
		codes[i] = int8(val)
	}
	return QuantizedVector{Min: min, Max: max, Codes: codes}
}

// DequantizeVector inverts QuantizeVector back to float32 — approximate due to int8 precision loss.
func DequantizeVector(qv QuantizedVector) []float32 {
	v := make([]float32, len(qv.Codes))
	if math.IsNaN(float64(qv.Min)) || math.IsNaN(float64(qv.Max)) ||
		math.IsInf(float64(qv.Min), 0) || math.IsInf(float64(qv.Max), 0) {
		return v
	}
	scale := (qv.Max - qv.Min) / 127
	if qv.Max == qv.Min {
		scale = 1
	}
	for i, c := range qv.Codes {
		v[i] = qv.Min + float32(c)*scale
	}
	return v
}

// QuantizedToBytes serializes a QuantizedVector to a 8+len(Codes) byte buffer.
func QuantizedToBytes(qv QuantizedVector) []byte {
	buf := make([]byte, 8+len(qv.Codes))
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(qv.Min))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(qv.Max))
	for i, c := range qv.Codes {
		buf[8+i] = byte(c)
	}
	return buf
}

// BytesToQuantized deserializes a byte buffer back into a QuantizedVector.
func BytesToQuantized(data []byte) (QuantizedVector, error) {
	if len(data) < 8 {
		return QuantizedVector{}, fmt.Errorf("quantized blob too short: %d bytes", len(data))
	}
	min := math.Float32frombits(binary.LittleEndian.Uint32(data[0:4]))
	max := math.Float32frombits(binary.LittleEndian.Uint32(data[4:8]))
	codes := make([]int8, len(data)-8)
	for i := range codes {
		codes[i] = int8(data[8+i])
	}
	return QuantizedVector{Min: min, Max: max, Codes: codes}, nil
}
