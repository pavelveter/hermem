package domain

// Episode captures the provenance of an extracted semantic claim.
type Episode struct {
	Fact
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	ExtractedFrom  string `json:"extracted_from,omitempty"`
}

// AsEpisode projects an Entity into its provenance fields.
func (e Entity) AsEpisode() Episode {
	return Episode{
		ConversationID: e.ConversationID,
		MessageID:      e.MessageID,
		ExtractedFrom:  e.ExtractedFrom,
	}
}
