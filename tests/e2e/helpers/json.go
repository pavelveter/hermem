package helpers

import (
	"encoding/json"
	"strings"
	"testing"
)

// AssertJSONField asserts that a JSON map has a specific value at a path.
func AssertJSONField(t *testing.T, m map[string]interface{}, path string, expected interface{}) {
	t.Helper()
	actual := getField(m, path)
	if actual == nil && expected != nil {
		t.Errorf("field %s: expected %v, got nil", path, expected)
		return
	}
	if actual != expected {
		t.Errorf("field %s: expected %v (%T), got %v (%T)", path, expected, expected, actual, actual)
	}
}

// AssertJSONFieldExists asserts that a JSON map has a non-nil value at a path.
func AssertJSONFieldExists(t *testing.T, m map[string]interface{}, path string) {
	t.Helper()
	if getField(m, path) == nil {
		t.Errorf("field %s: expected non-nil, got nil", path)
	}
}

// AssertJSONArray asserts that a JSON array has a specific length.
func AssertJSONArray(t *testing.T, m map[string]interface{}, path string, expectedLen int) {
	t.Helper()
	arr, ok := getField(m, path).([]interface{})
	if !ok {
		t.Errorf("field %s: expected array, got %T", path, getField(m, path))
		return
	}
	if len(arr) != expectedLen {
		t.Errorf("field %s: expected length %d, got %d", path, expectedLen, len(arr))
	}
}

// ParseJSON parses a JSON string into a map.
func ParseJSON(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	return m
}

// ParseJSONArray parses a JSON string into an array.
func ParseJSONArray(t *testing.T, s string) []interface{} {
	t.Helper()
	var arr []interface{}
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		t.Fatalf("parse JSON array: %v", err)
	}
	return arr
}

func getField(m map[string]interface{}, path string) interface{} {
	keys := splitPath(path)
	var current interface{} = m
	for _, key := range keys {
		cm, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = cm[key]
	}
	return current
}

func splitPath(path string) []string {
	return rangeSplit(path)
}

func rangeSplit(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ".")
}
