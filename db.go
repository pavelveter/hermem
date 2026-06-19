package main

import (
	"database/sql"
	"fmt"
	"math"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Entity struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Edge struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent performance
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Set synchronous to NORMAL for better performance
	if _, err := db.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set synchronous mode: %w", err)
	}

	// Create entities table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL CHECK(category IN ('world', 'experience', 'opinion', 'observation')),
			content TEXT NOT NULL,
			embedding BLOB,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create entities table: %w", err)
	}

	// Create edges table with composite primary key for deduplication
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS edges (
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			relation_type TEXT NOT NULL,
			PRIMARY KEY (source_id, target_id, relation_type),
			FOREIGN KEY (source_id) REFERENCES entities(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES entities(id) ON DELETE CASCADE
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create edges table: %w", err)
	}

	return db, nil
}

// EmbeddingToBytes converts a float32 slice to bytes for BLOB storage
func EmbeddingToBytes(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := math.Float32bits(v)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}

// BytesToEmbedding converts bytes from BLOB back to float32 slice
func BytesToEmbedding(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}

// CosineSimilarity computes cosine similarity between two vectors
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
