package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// playbackFrame is one ordered narrative frame in an episode
// playback. Distinct from TimelineEntry because playback is a
// presentation shape (what callers render) rather than a
// reconstruction shape (what the DB stores).
//
// Type is "event" | "memory" | "task" — matches TimelineEntryKind
// so callers can switch on Type uniformly across both types.
//
// Actor is a per-frame context field:
//   - event  → the event Type (message | action | observation | system)
//   - memory → the link role (extracted | referenced | mentioned)
//   - task   → the task status (pending | running | completed | failed)
//
// Content is the human-readable payload — identical across all
// three kinds so a single Markdown / text renderer can switch on
// Type alone.
type playbackFrame struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Actor     string    `json:"actor,omitempty"`
	Content   string    `json:"content"`
}

// playbackService renders an episode as a chronological narrative.
type playbackService struct {
	db *sql.DB
}

// newPlaybackService constructs a playbackService. db is required.
func newPlaybackService(db *sql.DB) *playbackService {
	return &playbackService{db: db}
}

// Playback returns the episode's chronological frames in a
// presentation-ready shape. Delegates the merge + sort to
// TimelineService so the ordering matches what /query and other
// timeline-shaped surfaces already produce.
func (p *playbackService) Playback(ctx context.Context, episodeID string) ([]playbackFrame, error) {
	if episodeID == "" {
		return nil, fmt.Errorf("episodic: Playback: episode_id required")
	}
	entries, err := NewTimelineService(p.db).ReconstructTimeline(ctx, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: Playback reconstruct: %w", err)
	}
	frames := make([]playbackFrame, len(entries))
	for i, e := range entries {
		frames[i] = playbackFrame{
			Timestamp: e.Timestamp,
			Type:      string(e.Kind),
			Source:    e.SourceID,
			Actor:     e.Type, // event Type / memory role / task status
			Content:   e.Content,
		}
	}
	return frames, nil
}

// ExportJSON marshals the frames as indented JSON bytes. The
// envelope is a plain array — callers wrap with their own outer
// keys (e.g. episode_id, generated_at) if they need context.
func (p *playbackService) ExportJSON(frames []playbackFrame) ([]byte, error) {
	if frames == nil {
		frames = []playbackFrame{}
	}
	out, err := json.MarshalIndent(frames, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("episodic: ExportJSON marshal: %w", err)
	}
	return out, nil
}

// ExportMarkdown renders the frames as a Markdown timeline:
// "# Episode Timeline\n\n- timestamp — actor (type) content\n\n..."
// Ordered chronologically (caller passes frames in the desired
// order — Playback already returns them sorted).
func (p *playbackService) ExportMarkdown(frames []playbackFrame) string {
	var b strings.Builder
	b.WriteString("# Episode Timeline\n\n")
	for _, f := range frames {
		actor := f.Actor
		if actor != "" {
			fmt.Fprintf(&b, "- `%s` — **%s** (%s): %s\n", f.Timestamp.Format(time.RFC3339), actor, f.Type, f.Content)
		} else {
			fmt.Fprintf(&b, "- `%s` — (%s): %s\n", f.Timestamp.Format(time.RFC3339), f.Type, f.Content)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// ExportText renders the frames as a plain-text narrative with one
// frame per line. Useful for logs or copy-paste into chat UIs that
// don't render Markdown.
func (p *playbackService) ExportText(frames []playbackFrame) string {
	var b strings.Builder
	for _, f := range frames {
		actor := f.Actor
		if actor != "" {
			fmt.Fprintf(&b, "[%s] %s/%s: %s\n", f.Timestamp.Format(time.RFC3339), f.Type, actor, f.Content)
		} else {
			fmt.Fprintf(&b, "[%s] %s: %s\n", f.Timestamp.Format(time.RFC3339), f.Type, f.Content)
		}
	}
	return b.String()
}
