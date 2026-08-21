package domain

// MemoryMessage is a single dialog turn to be ingested by the memory pipeline.
//
// It is canonically part of the public domain vocabulary (an external
// producer can publish messages onto the in-memory channel or the
// durable pending.jsonl drain file written by MemoryWorkerResilient).
// JSON tags are stable so producers written in any language can replay
// the queue on restart.
//
// Canonicalised from the legacy core.MemoryMessage struct as part of
// task 4.3 (services off core). The legacy alias remains in
// src/internal/core/types.go for one release.
type MemoryMessage struct {
	Dialog         string `json:"dialog"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
}
