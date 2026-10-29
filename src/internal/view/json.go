package view

import (
	"encoding/json"
	"io"
)

// writeJSON encodes v to w. Split out so the handler stays about routing.
func writeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}
