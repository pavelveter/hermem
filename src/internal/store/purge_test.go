package store

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// bufferedHandler captures slog records into a slice. Used by the
// nil-vi warn test to assert the warn fires with the expected fields
// WITHOUT coupling to stderr output that would dirty `-v` test logs.
//
// LIMITATION: WithAttrs / WithGroup are no-op pass-throughs that
// return the bare handler. A future test asserting on chained fields
// (e.g. slog.New(buf).With("k", "v").Warn("msg") and then checking
// that the record carries k=v) would silently drop the chained
// fields because the returned `Handler` is the unparented `h`. Pass
// attrs inline via slog.Warn("msg", slog.String(...)) instead.
type bufferedHandler struct {
	records []slog.Record
}

func (h *bufferedHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *bufferedHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *bufferedHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *bufferedHandler) WithGroup(_ string) slog.Handler      { return h }

// TestPurgeEntity_NotFoundReturnsSentinel — calling PurgeEntity on a
// non-existent id must return an error wrapped with
// ErrPurgeEntityNotFound so callers can branch with errors.Is without
// string-matching. Locks the § 3.3 contract.
func TestPurgeEntity_NotFoundReturnsSentinel(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	err := PurgeEntity(ctx, db, nil, "nonexistent-id-xyz")
	if err == nil {
		t.Fatal("PurgeEntity on missing id: want error, got nil")
	}
	if !errors.Is(err, ErrPurgeEntityNotFound) {
		t.Fatalf("PurgeEntity on missing id: want errors.Is(ErrPurgeEntityNotFound)=true; got err=%v", err)
	}
}

// TestPurgeEntity_NilVILogsAndReturnsNil — when vi is nil and a real
// DB delete succeeds, PurgeEntity must (a) return nil so callers see
// the deletion as successful, AND (b) emit a single WARN-level slog
// record carrying the entity id and db_purged=true field so an
// operator can recover via algo.ReEmbedAll.
//
//	// INTENTIONALLY NOT t.Parallel — this test swaps the package-global
//
// slog.Default() handler for the duration. A parallel sibling in
// the same package would observe either the stale handler or a
// stale SetDefault restore and fail to capture its own warn. The
// store_test bootstrap runs every test serially against MemDBRandom
// so the cost of one non-parallel test is small.
//
// (The slog-parallel-leakage concern was raised by review; the
// non-leaky contract is: this test never invokes t.Parallel and
// the defer `slog.SetDefault(prev)` always restores the prior
// handler before returning — so a sibling test, parallel or
// sequential, sees whichever handler the package default holds at
// the moment it calls slog.Default().)
func TestPurgeEntity_NilVILogsAndReturnsNil(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	const id = "test-entity-purge"
	seedEntity(t, db, id, "concept", "entity content")

	// Swap the default slog handler for the test duration to capture
	// warn records without polluting test output.
	buf := &bufferedHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(buf))
	defer slog.SetDefault(prev)

	if err := PurgeEntity(ctx, db, nil, id); err != nil {
		t.Fatalf("PurgeEntity(nil vi) on real id: want nil error, got %v", err)
	}

	// Find the WARN record about vi-is-nil and assert it carries
	// entity_id=id and db_purged=true.
	var warnRec *slog.Record
	for i := range buf.records {
		if buf.records[i].Level == slog.LevelWarn &&
			strings.Contains(buf.records[i].Message, "vi is nil") {
			warnRec = &buf.records[i]
			break
		}
	}
	if warnRec == nil {
		t.Fatalf("expected slog.Warn(\"vi is nil\") record; got %d total records, none with WARN+message substring", len(buf.records))
	}
	var gotEntityID string
	var gotDBPurged bool
	warnRec.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "entity_id":
			gotEntityID = a.Value.String()
		case "db_purged":
			gotDBPurged = a.Value.Bool()
		}
		return true
	})
	if gotEntityID != id {
		t.Fatalf("slog entity_id: want %q, got %q", id, gotEntityID)
	}
	if !gotDBPurged {
		t.Fatalf("slog db_purged: want true, got false")
	}
	// DB-side audit: row should be gone.
	var rowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, id).Scan(&rowCount); err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("entity row: want 0 rows after purge, got %d", rowCount)
	}
}
