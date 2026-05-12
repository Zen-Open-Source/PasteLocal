package server

import (
	"sync"
	"time"
)

// RateLimiter implements a token-bucket rate limiter.
// Tokens are replenished at a steady rate (rate per second) up to maxTokens.
// Each call to Allow consumes one token if available.
type RateLimiter struct {
	mu        sync.Mutex
	tokens    float64
	maxTokens float64
	rate      float64 // tokens per second
	lastTime  time.Time
}

// NewRateLimiter creates a RateLimiter that allows up to perMinute requests
// per minute. The bucket starts full.
func NewRateLimiter(perMinute int) *RateLimiter {
	rate := float64(perMinute) / 60.0
	maxTokens := float64(perMinute)
	return &RateLimiter{
		tokens:    maxTokens,
		maxTokens: maxTokens,
		rate:      rate,
		lastTime:  time.Now(),
	}
}

// Allow attempts to consume one token. It returns true if the request is
// allowed, false if the bucket is empty (rate limit exceeded).
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastTime).Seconds()
	r.lastTime = now

	// Replenish tokens based on elapsed time.
	r.tokens += elapsed * r.rate
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
	}

	if r.tokens < 1.0 {
		return false
	}

	r.tokens -= 1.0
	return true
}
