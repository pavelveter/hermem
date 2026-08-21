package core

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

func FuzzStoreRequestJSONRoundTrip(f *testing.F) {
	f.Add("e1", "world", "Paris is the capital of France")
	f.Add("", "", "")

	f.Fuzz(func(t *testing.T, id, category, content string) {
		if !utf8.ValidString(id) || !utf8.ValidString(category) || !utf8.ValidString(content) {
			t.Skip()
		}

		req := StoreRequest{
			ID:       id,
			Category: category,
			Content:  content,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Skip()
		}

		var req2 StoreRequest
		if err := json.Unmarshal(data, &req2); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if req.ID != req2.ID || req.Category != req2.Category || req.Content != req2.Content {
			t.Errorf("mismatch after round-trip")
		}
	})
}
