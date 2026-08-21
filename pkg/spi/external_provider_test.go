package spi_test

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/pkg/spi/testdata/externalprovider"
)

func TestExternalLikeProviderUsesOnlyPublicContracts(t *testing.T) {
	provider := externalprovider.Provider{}
	if _, err := provider.Embed(context.Background(), "text"); err != nil {
		t.Fatal(err)
	}
	hits, err := provider.Search(context.Background(), spi.SearchRequest{Namespace: "external", Vector: []float32{1}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "external" {
		t.Fatalf("hits = %+v", hits)
	}
}
