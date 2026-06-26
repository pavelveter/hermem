package episodic

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Summarizer produces a text summary for an episode by collecting
// every event + linked memory on the episode, stitching them into
// a synthetic dialog, handing it to an LLMExtractor, and storing
// the extracted entities back as the episode's summary column.
//
// Flat-package + stateless pattern. Depends on the same Service
// types it summarises (Episode / Event / LinkService) so callers
// pass one *sql.DB and get a working summarizer without manual
// wiring.
type Summarizer struct {
	db        *sql.DB
	extractor core.LLMExtractor
}

// NewSummarizer constructs a Summarizer. Both db and extractor are
// required — summarisation is intrinsically an LLM-backed
// operation; without an extractor callers should not be here.
func NewSummarizer(db *sql.DB, extractor core.LLMExtractor) *Summarizer {
	return &Summarizer{db: db, extractor: extractor}
}

// SummarizeEpisode produces a summary for the episode with the
// given id and stores it back in the episodes.summary column.
// Returns the generated summary text so callers don't need a
// second GetEpisode round-trip.
//
// Pipeline:
//   1. Load the episode (ErrNotFound if missing).
//   2. Load every event on the episode (ORDER BY timestamp ASC).
//   3. Load every linked memory (one row per (episode, entity, role)).
//   4. Stitch events + memories into a synthetic dialog.
//   5. Hand the dialog to extractor.ExtractEntities.
//   6. Format the extracted entities as a bulleted summary.
//   7. Persist via UpdateSummary on the episode.
//
// Errors at any step propagate up with a wrapped context.
func (s *Summarizer) SummarizeEpisode(ctx context.Context, episodeID string) (string, error) {
	if episodeID == "" {
		return "", fmt.Errorf("episodic: SummarizeEpisode: episode_id required")
	}
	epSvc := New(s.db)
	if _, err := epSvc.GetEpisode(ctx, episodeID); err != nil {
		return "", fmt.Errorf("episodic: SummarizeEpisode load episode: %w", err)
	}

	evSvc := NewEventService(s.db)
	events, err := evSvc.ListEventsByEpisode(ctx, episodeID)
	if err != nil {
		return "", fmt.Errorf("episodic: SummarizeEpisode load events: %w", err)
	}

	linkSvc := NewLinkService(s.db)
	memories, err := linkSvc.ListMemoriesForEpisode(ctx, episodeID)
	if err != nil {
		return "", fmt.Errorf("episodic: SummarizeEpisode load memories: %w", err)
	}

	dialog := buildSummarizationDialog(events, memories)
	if dialog == "" {
		// Nothing to summarise — record an explicit empty summary
		// so callers can tell "summarised empty" from
		// "never summarised".
		if err := epSvc.UpdateSummary(ctx, episodeID, ""); err != nil {
			return "", fmt.Errorf("episodic: SummarizeEpisode persist empty: %w", err)
		}
		return "", nil
	}

	result, err := s.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return "", fmt.Errorf("episodic: SummarizeEpisode extract: %w", err)
	}
	summary := formatSummaryFromExtraction(result)
	if err := epSvc.UpdateSummary(ctx, episodeID, summary); err != nil {
		return "", fmt.Errorf("episodic: SummarizeEpisode persist: %w", err)
	}
	return summary, nil
}

// buildSummarizationDialog stitches the inputs into a deterministic
// text blob the LLM extractor can read. Each event renders as
// "[type] content"; each memory renders as "[memory:role]
// content". Order matches the load order (events ASC by timestamp,
// then memories ASC by linked_at).
func buildSummarizationDialog(events []Event, memories []MemoryRef) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "[%s] %s\n", e.Type, e.Content)
	}
	for _, m := range memories {
		fmt.Fprintf(&b, "[memory:%s] %s\n", m.Role, m.Content)
	}
	return b.String()
}

// formatSummaryFromExtraction renders the extracted entities as
// a bulleted summary. Empty result renders as the literal string
// "(no entities extracted)" so the column is never empty when
// the extractor ran successfully — distinct from "never
// summarised".
func formatSummaryFromExtraction(result *core.ExtractionResult) string {
	if result == nil || len(result.Entities) == 0 {
		return "(no entities extracted)"
	}
	var b strings.Builder
	for _, ent := range result.Entities {
		fmt.Fprintf(&b, "- [%s] %s\n", ent.Category, ent.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
