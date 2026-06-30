package store

import "testing"

func FuzzSplitSQL(f *testing.F) {
	f.Add("SELECT 1;\nSELECT 2;\n")
	f.Add("CREATE TABLE t (id INT);\nINSERT INTO t VALUES (1);\n")
	f.Add("-- comment\nSELECT 1;\n")
	f.Add("CREATE TRIGGER IF NOT EXISTS foo\nBEGIN\n  SELECT 1;\nEND;\n")
	f.Add("")
	f.Add("SELECT 1; SELECT 2; SELECT 3;")

	f.Fuzz(func(t *testing.T, input string) {
		result := splitSQL(input)
		for _, stmt := range result {
			if stmt == "" {
				t.Errorf("splitSQL produced empty statement from input %q", input)
			}
		}
	})
}

func FuzzEmbeddingRoundTrip(f *testing.F) {
	f.Add(float32(1), float32(2), float32(3))
	f.Add(float32(0), float32(0), float32(0))
	f.Add(float32(-1), float32(0.5), float32(1e10))

	f.Fuzz(func(t *testing.T, a, b, c float32) {
		vals := []float32{a, b, c}
		blob := EmbeddingToBytes(vals)
		got := BytesToEmbedding(blob)
		if len(got) != len(vals) {
			t.Errorf("length mismatch: want %d, got %d", len(vals), len(got))
		}
		for i := range vals {
			if vals[i] != got[i] {
				t.Errorf("value mismatch at %d: want %v, got %v", i, vals[i], got[i])
			}
		}
	})
}
