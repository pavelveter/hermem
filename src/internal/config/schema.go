package config

import (
	"fmt"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/pavelveter/hermem/src/internal/core"
)

// DefaultSchemaConfig returns a SchemaConfig with built-in defaults.
// Delegates to core.DefaultSchemaConfig for the canonical definition.
func DefaultSchemaConfig(stateful bool) core.SchemaConfig {
	return core.DefaultSchemaConfig(stateful)
}

// ValidateSchema checks a SchemaConfig for internal consistency.
func ValidateSchema(s core.SchemaConfig) error {
	if len(s.AllowedCategories) == 0 {
		return fmt.Errorf("allowed_categories must not be empty")
	}
	if len(s.AllowedRelations) == 0 {
		return fmt.Errorf("allowed_relations must not be empty")
	}
	seen := make(map[string]bool, len(s.ValidStateOrder))
	for _, state := range s.ValidStateOrder {
		if seen[state] {
			return fmt.Errorf("duplicate state %q in valid_states", state)
		}
		seen[state] = true
	}
	if len(s.StatefulCategories) > 0 && len(s.ValidStateOrder) == 0 {
		return fmt.Errorf("stateful_categories set but valid_states is empty")
	}
	if s.StateUnblocking != "" && len(s.ValidStates) > 0 && !s.ValidStates[s.StateUnblocking] {
		return fmt.Errorf("state_unblocking %q is not in valid_states", s.StateUnblocking)
	}
	for _, rel := range []string{s.RelationBlocking, s.RelationRecovery} {
		if rel != "" && len(s.AllowedRelations) > 0 && !s.AllowedRelations[rel] {
			return fmt.Errorf("schema relation %q is not in allowed_relations", rel)
		}
	}
	return nil
}

// ParseSchemaSection parses the [schema] section of hermem.ini with detailed error messages.
func ParseSchemaSection(section *ini.Section, path string) (core.SchemaConfig, error) {
	allowedKeys := map[string]bool{
		"allowed_categories":  true,
		"allowed_relations":   true,
		"stateful_categories": true,
		"valid_states":        true,
		"relation_blocking":   true,
		"state_unblocking":    true,
		"relation_recovery":   true,
	}
	for _, k := range section.Keys() {
		name := strings.ToLower(k.Name())
		if name == "name" {
			continue
		}
		if !allowedKeys[name] {
			return core.SchemaConfig{}, fmt.Errorf("%s:%d: unknown [schema] key %q", path, FindConfigLine(path, k.Name()), k.Name())
		}
	}
	schema := DefaultSchemaConfig(true)
	if v := ParseCSVList(section.Key("allowed_categories").String()); len(v) > 0 {
		schema.AllowedCategories = BoolMap(v)
	} else {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: [schema].allowed_categories must not be empty", path, FindConfigLine(path, "allowed_categories"))
	}
	if v := ParseCSVList(section.Key("allowed_relations").String()); len(v) > 0 {
		schema.AllowedRelations = BoolMap(v)
	} else {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: [schema].allowed_relations must not be empty", path, FindConfigLine(path, "allowed_relations"))
	}
	stateful := ParseCSVList(section.Key("stateful_categories").String())
	schema.StatefulCategories = BoolMap(stateful)
	states := ParseCSVList(section.Key("valid_states").String())
	schema.ValidStateOrder = states
	schema.ValidStates = BoolMap(states)
	if len(stateful) > 0 && len(states) == 0 {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: [schema].valid_states required when stateful_categories is set", path, FindConfigLine(path, "valid_states"))
	}
	for category := range schema.StatefulCategories {
		if !schema.AllowedCategories[category] {
			return core.SchemaConfig{}, fmt.Errorf("%s:%d: stateful category %q is not in allowed_categories", path, FindConfigLine(path, "stateful_categories"), category)
		}
	}
	if v := strings.TrimSpace(section.Key("relation_blocking").String()); v != "" {
		schema.RelationBlocking = v
	}
	if v := strings.TrimSpace(section.Key("state_unblocking").String()); v != "" {
		schema.StateUnblocking = v
	}
	if v := strings.TrimSpace(section.Key("relation_recovery").String()); v != "" {
		schema.RelationRecovery = v
	}
	for _, rel := range []string{schema.RelationBlocking, schema.RelationRecovery} {
		if rel != "" && !schema.AllowedRelations[rel] {
			return core.SchemaConfig{}, fmt.Errorf("%s:%d: schema relation %q is not in allowed_relations", path, FindConfigLine(path, rel), rel)
		}
	}
	if schema.StateUnblocking != "" && len(schema.ValidStates) > 0 && !schema.ValidStates[schema.StateUnblocking] {
		return core.SchemaConfig{}, fmt.Errorf("%s:%d: state_unblocking %q is not in valid_states", path, FindConfigLine(path, "state_unblocking"), schema.StateUnblocking)
	}
	return schema, nil
}
