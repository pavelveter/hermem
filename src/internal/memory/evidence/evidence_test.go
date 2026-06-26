package evidence_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/memory/evidence"
	"github.com/pavelveter/hermem/src/internal/store"
)

var extCtx = context.Background()

func TestService_CreateEvidence_Success(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	e := &evidence.Evidence{BeliefID: b, Polarity: evidence.PolaritySupport, Strength: 0.7, Content: "supporting", SourceKind: "test"}
	if err := svc.CreateEvidence(extCtx, e); err != nil {
		t.Fatalf("CreateEvidence: %v", err)
	}
	if e.ID <= 0 {
		t.Fatal("expected ID set after create")
	}
	got, err := svc.GetEvidence(extCtx, e.ID)
	if err != nil {
		t.Fatalf("GetEvidence: %v", err)
	}
	if got.Content != "supporting" || got.Polarity != evidence.PolaritySupport {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestService_CreateEvidence_RejectsNilOrEmpty(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	if err := svc.CreateEvidence(extCtx, nil); err == nil {
		t.Fatal("expected nil-rejection")
	}
	b := mustCreateBeliefRow(t, db)
	if err := svc.CreateEvidence(extCtx, &evidence.Evidence{BeliefID: b, Polarity: evidence.PolaritySupport, Content: ""}); err == nil {
		t.Fatal("expected empty-content rejection")
	}
}

func TestService_CreateEvidence_RejectsInvalidPolarity(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	err := svc.CreateEvidence(extCtx, &evidence.Evidence{BeliefID: b, Polarity: evidence.Polarity("garbage"), Content: "x", Strength: 0.5})
	if !errors.Is(err, evidence.ErrInvalidPolarity) {
		t.Fatalf("expected ErrInvalidPolarity, got %v", err)
	}
}

func TestService_CreateEvidence_RejectsOutOfBoundsStrength(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	if err := svc.CreateEvidence(extCtx, &evidence.Evidence{BeliefID: b, Polarity: evidence.PolaritySupport, Strength: 1.5, Content: "x"}); err == nil {
		t.Fatal("expected >1 rejection")
	}
	if err := svc.CreateEvidence(extCtx, &evidence.Evidence{BeliefID: b, Polarity: evidence.PolaritySupport, Strength: -0.1, Content: "x"}); err == nil {
		t.Fatal("expected negative rejection")
	}
}

func TestService_CreateEvidence_ForgivingStrengthDefault(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	e := &evidence.Evidence{BeliefID: b, Polarity: evidence.PolarityRefute, Strength: 0, Content: "x"}
	if err := svc.CreateEvidence(extCtx, e); err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.Strength != 1.0 {
		t.Fatalf("expected Strength defaulted to 1.0, got %v", e.Strength)
	}
}

func TestService_GetEvidence_Ok(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	e := mustCreateEvidence(t, svc, b, "sample")
	got, err := svc.GetEvidence(extCtx, e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != "sample" || got.BeliefID != b {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestService_GetEvidence_NotFound(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	if _, err := svc.GetEvidence(extCtx, 0); !errors.Is(err, evidence.ErrNotFound) {
		t.Fatalf("expected NotFound on id<=0, got %v", err)
	}
	if _, err := svc.GetEvidence(extCtx, 9999); !errors.Is(err, evidence.ErrNotFound) {
		t.Fatalf("expected NotFound on missing id, got %v", err)
	}
}

func TestService_ListForBelief_OrdersByCreatedAt(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	c1 := mustCreateEvidenceContents(t, svc, b, "first")
	time.Sleep(5 * time.Millisecond)
	c2 := mustCreateEvidenceContents(t, svc, b, "second")
	list, err := svc.ListForBelief(extCtx, b)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != c1.ID || list[1].ID != c2.ID {
		t.Fatalf("order mismatch: ids %d,%d got %v", c1.ID, c2.ID, list)
	}
}

func TestService_ListForBelief_EmptyForMissingBelief(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	list, err := svc.ListForBelief(extCtx, 99999)
	if err != nil {
		t.Fatalf("expected nil err on missing belief, got %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestService_UpdateStrength_Success(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	e := mustCreateEvidence(t, svc, b, "fixed")
	if err := svc.UpdateStrength(extCtx, e.ID, 0.42); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := svc.GetEvidence(extCtx, e.ID)
	if got.Strength < 0.41 || got.Strength > 0.43 {
		t.Fatalf("expected ~0.42, got %v", got.Strength)
	}
}

func TestService_UpdateStrength_OutOfBoundsRejection(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	e := mustCreateEvidence(t, svc, b, "fixed")
	if err := svc.UpdateStrength(extCtx, e.ID, -0.1); err == nil {
		t.Fatal("expected negative rejection")
	}
	if err := svc.UpdateStrength(extCtx, e.ID, 1.01); err == nil {
		t.Fatal("expected >1 rejection")
	}
}

func TestService_DeleteEvidence_Success(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	e := mustCreateEvidence(t, svc, b, "fix")
	if err := svc.DeleteEvidence(extCtx, e.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetEvidence(extCtx, e.ID); !errors.Is(err, evidence.ErrNotFound) {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}

func TestService_DeleteEvidence_NotFound(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	if err := svc.DeleteEvidence(extCtx, 9999); !errors.Is(err, evidence.ErrNotFound) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestService_CascadeDelete_WhenBeliefRemoved(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	mustCreateEvidence(t, svc, b, "a")
	mustCreateEvidence(t, svc, b, "b")
	pre, _ := svc.ListForBelief(extCtx, b)
	if len(pre) != 2 {
		t.Fatalf("expected 2 evidence rows pre-cascade, got %d", len(pre))
	}
	if _, err := db.Exec(`DELETE FROM beliefs WHERE id = ?`, b); err != nil {
		t.Fatalf("raw delete belief: %v", err)
	}
	post, _ := svc.ListForBelief(extCtx, b)
	if len(post) != 0 {
		t.Fatalf("expected cascade to wipe evidence, got %d rows", len(post))
	}
}

func TestService_ConcurrentCreate_RaceSafe(t *testing.T) {
	db, _ := store.MemDB()
	svc := evidence.New(db)
	b := mustCreateBeliefRow(t, db)
	const N = 32
	var wg sync.WaitGroup
	ids := make([]int64, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			e := &evidence.Evidence{BeliefID: b, Polarity: evidence.PolaritySupport, Strength: 0.5, Content: "conc-" + string(rune('a'+idx))}
			if err := svc.CreateEvidence(extCtx, e); err == nil {
				ids[idx] = e.ID
			}
		}(i)
	}
	wg.Wait()
	seen := map[int64]bool{}
	uniq := 0
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		uniq++
	}
	if uniq != N {
		t.Fatalf("expected %d unique IDs, got %d", N, uniq)
	}
}

// ---- helpers ----
func mustCreateBeliefRow(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO beliefs (content) VALUES ('test-belief')`)
	if err != nil {
		t.Fatalf("insert belief: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("lastid belief: %v", err)
	}
	return id
}

func mustCreateEvidence(t *testing.T, svc evidence.Service, beliefID int64, content string) *evidence.Evidence {
	t.Helper()
	return mustCreateEvidenceContents(t, svc, beliefID, content)
}

func mustCreateEvidenceContents(t *testing.T, svc evidence.Service, beliefID int64, content string) *evidence.Evidence {
	t.Helper()
	e := &evidence.Evidence{BeliefID: beliefID, Polarity: evidence.PolaritySupport, Strength: 0.5, Content: content, SourceKind: "test"}
	if err := svc.CreateEvidence(extCtx, e); err != nil {
		t.Fatalf("createEvidence: %v", err)
	}
	return e
}
