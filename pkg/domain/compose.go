package domain

// Compose reassembles a full Entity from its domain projections.
func Compose(f Fact, ev Evidence, ep Episode, t Task, b Belief) Entity {
	return Entity{
		ID: f.ID, Category: f.Category, Content: f.Content, Embedding: f.Embedding,
		Confidence: ev.Confidence, Source: ev.Source, SourceType: ev.SourceType,
		ConversationID: ep.ConversationID, MessageID: ep.MessageID, ExtractedFrom: ep.ExtractedFrom,
		Status: t.Status, ValidFrom: t.ValidFrom, ValidTo: t.ValidTo, Priority: t.Priority,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, LastAccessedAt: b.LastAccessedAt,
		Archived: b.Archived, Degree: b.Degree,
	}
}
