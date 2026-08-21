package domain_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
)

// TestPublicTypesDoNotLeakInternalTypes guards the domain package's exported
// surface. The public contract must never expose database/sql, sqlite, or
// src/internal types, so external consumers can compile against pkg/domain
// without pulling the runtime implementation into their module graph.
func TestPublicTypesDoNotLeakInternalTypes(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(domain.Entity{}),
		reflect.TypeOf(domain.Edge{}),
		reflect.TypeOf(domain.SchemaConfig{}),
		reflect.TypeOf(domain.EntityDraft{}),
		reflect.TypeOf(domain.RelationDraft{}),
		reflect.TypeOf(domain.Error{}),
	}
	for _, typ := range types {
		checkStructLeaks(t, typ)
	}
}

// TestPublicIdentifiersAreConstructible pins that domain identity types remain
// constructible by external consumers during the compatibility migration.
func TestPublicIdentifiersAreConstructible(t *testing.T) {
	var entityID domain.EntityID = "entity-1"
	var taskID domain.TaskID = "task-1"
	if entityID == "" || taskID == "" {
		t.Fatal("public identifiers must be constructible by external consumers")
	}
}

func checkStructLeaks(t *testing.T, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		ft := f.Type
		// Unwrap pointers/slices so the element type is inspected.
		ft = elemType(ft)
		pkg := ft.PkgPath()
		if pkg == "" {
			continue
		}
		if strings.Contains(pkg, "database/sql") ||
			strings.Contains(pkg, "sqlite") ||
			strings.Contains(pkg, "/internal/") {
			t.Errorf("%s.%s leaks implementation type %s", typ.Name(), f.Name, ft)
		}
	}
}

func elemType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t
}
