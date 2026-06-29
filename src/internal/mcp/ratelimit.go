package mcp

import (
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter.
// Tokens are added at a fixed rate up to a maximum.
// Each request consumes one token. If no tokens are available,
// the request is rejected.
type RateLimiter struct {
	mu        sync.Mutex
	tokens    float64
	maxTokens float64
	rate      float64 // tokens per second
	lastTime  time.Time
}

// NewRateLimiter creates a rate limiter with the given max tokens
// and refill rate (tokens per second).
func NewRateLimiter(maxTokens int, ratePerSecond float64) *RateLimiter {
	return &RateLimiter{
		tokens:    float64(maxTokens),
		maxTokens: float64(maxTokens),
		rate:      ratePerSecond,
		lastTime:  time.Now(),
	}
}

// Allow returns true if a token is available. Consumes one token.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastTime = now

	if rl.tokens < 1 {
		return false
	}
	rl.tokens--
	return true
}

// Reset returns all tokens to max. Used after error responses
// to give the client a fresh start.
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.tokens = rl.maxTokens
}
