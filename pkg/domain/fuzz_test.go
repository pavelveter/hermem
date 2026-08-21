package domain

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

func FuzzEntityJSONRoundTrip(f *testing.F) {
	f.Add("test-id", "world", "test content")
	f.Add("", "", "")
	f.Add("id-123", "task", "A task with special chars: <>&\"'")

	f.Fuzz(func(t *testing.T, id, category, content string) {
		if !utf8.ValidString(id) || !utf8.ValidString(category) || !utf8.ValidString(content) {
			t.Skip()
		}

		e := Entity{
			ID:       id,
			Category: category,
			Content:  content,
		}

		data, err := json.Marshal(e)
		if err != nil {
			t.Skip()
		}

		var e2 Entity
		if err := json.Unmarshal(data, &e2); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if e.ID != e2.ID {
			t.Errorf("ID mismatch: %q != %q", e.ID, e2.ID)
		}
		if e.Category != e2.Category {
			t.Errorf("Category mismatch: %q != %q", e.Category, e2.Category)
		}
		if e.Content != e2.Content {
			t.Errorf("Content mismatch: %q != %q", e.Content, e2.Content)
		}
	})
}
