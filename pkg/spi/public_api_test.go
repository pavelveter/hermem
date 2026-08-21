package spi_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
)

// TestPublicInterfacesAreFrozen pins the exported SPI interface method sets.
// Adding a required method to one of these interfaces is a breaking public
// change and must fail this snapshot until it is versioned.
func TestPublicInterfacesAreFrozen(t *testing.T) {
	cases := []struct {
		name     string
		typ      reflect.Type
		expected []string
	}{
		{name: "Embedder", typ: reflect.TypeOf((*spi.Embedder)(nil)).Elem(), expected: []string{"Embed"}},
		{name: "BatchEmbedder", typ: reflect.TypeOf((*spi.BatchEmbedder)(nil)).Elem(), expected: []string{"EmbedBatch"}},
		{name: "Extractor", typ: reflect.TypeOf((*spi.Extractor)(nil)).Elem(), expected: []string{"Extract"}},
		{name: "Reranker", typ: reflect.TypeOf((*spi.Reranker)(nil)).Elem(), expected: []string{"Rerank"}},
		{
			name: "VectorStore",
			typ:  reflect.TypeOf((*spi.VectorStore)(nil)).Elem(),
			expected: []string{
				"Search", "Upsert", "Delete", "Stats",
			},
		},
	}
	for _, tc := range cases {
		methods := make([]string, 0, tc.typ.NumMethod())
		for i := 0; i < tc.typ.NumMethod(); i++ {
			methods = append(methods, tc.typ.Method(i).Name)
		}
		if !equalStringSet(methods, tc.expected) {
			t.Errorf("%s method set drifted: got %v want %v", tc.name, methods, tc.expected)
		}
	}
}

// TestPublicPackagesDoNotExposeInternalTypes checks that the public SPI
// package's exported interface signatures do not reference database/sql,
// sqlite, or internal package types.
func TestPublicPackagesDoNotExposeInternalTypes(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf((*spi.Embedder)(nil)).Elem(),
		reflect.TypeOf((*spi.Extractor)(nil)).Elem(),
		reflect.TypeOf((*spi.Reranker)(nil)).Elem(),
		reflect.TypeOf((*spi.VectorStore)(nil)).Elem(),
	} {
		for i := 0; i < typ.NumMethod(); i++ {
			sig := typ.Method(i).Type
			for j := 0; j < sig.NumIn(); j++ {
				assertPublicType(t, sig.In(j))
			}
			for j := 0; j < sig.NumOut(); j++ {
				assertPublicType(t, sig.Out(j))
			}
		}
	}
}

func assertPublicType(t *testing.T, typ reflect.Type) {
	t.Helper()
	pkg := typ.PkgPath()
	if pkg == "" {
		return
	}
	if strings.Contains(pkg, "database/sql") ||
		strings.Contains(pkg, "sqlite") ||
		strings.Contains(pkg, "/internal/") {
		t.Errorf("public SPI leaks implementation type %s", typ)
	}
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, item := range a {
		seen[item] = true
	}
	for _, item := range b {
		if !seen[item] {
			return false
		}
	}
	return true
}
