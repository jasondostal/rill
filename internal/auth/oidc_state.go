package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// OIDCStateEntry holds PKCE state for an in-flight OIDC flow.
type OIDCStateEntry struct {
	CodeVerifier  string
	Nonce         string
	RedirectAfter string
	CreatedAt     time.Time
}

// OIDCStateStore is an in-memory store with TTL for OIDC state → code_verifier mapping.
// Single-process safe. For multi-worker deployments, replace with Redis or a DB table.
type OIDCStateStore struct {
	mu      sync.Mutex
	store   map[string]*OIDCStateEntry
	ttl     time.Duration
	cleanup time.Time
	cancel  context.CancelFunc
}

// NewOIDCStateStore creates a state store with the given TTL (default 10 minutes).
// Spawns a background goroutine that purges expired entries every minute so
// abandoned flows don't leak memory between user logins. Stop it with Close.
func NewOIDCStateStore(ttl time.Duration) *OIDCStateStore {
	return NewOIDCStateStoreWithContext(context.Background(), ttl)
}

// NewOIDCStateStoreWithContext is like NewOIDCStateStore but ties the purge
// goroutine's lifetime to the parent context (for tests / clean shutdown).
func NewOIDCStateStoreWithContext(parent context.Context, ttl time.Duration) *OIDCStateStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	ctx, cancel := context.WithCancel(parent)
	s := &OIDCStateStore{
		store:   make(map[string]*OIDCStateEntry),
		ttl:     ttl,
		cleanup: time.Now(),
		cancel:  cancel,
	}
	go s.runPurger(ctx)
	return s
}

// Close stops the background purge goroutine. Safe to call multiple times.
func (s *OIDCStateStore) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// runPurger ticks every minute and cleans expired entries.
func (s *OIDCStateStore) runPurger(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			s.purge()
			s.mu.Unlock()
		}
	}
}

// Create generates a new (state, code_verifier, nonce) triple and stores it.
// The nonce is bound to the state so the callback can reject a replayed
// id_token by comparing claims.Nonce to the persisted value.
func (s *OIDCStateStore) Create(redirectAfter string) (state, codeVerifier, nonce string) {
	state = randomString(32)
	codeVerifier = randomString(64)
	nonce = randomString(32)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purge()
	s.store[state] = &OIDCStateEntry{
		CodeVerifier:  codeVerifier,
		Nonce:         nonce,
		RedirectAfter: redirectAfter,
		CreatedAt:     time.Now(),
	}
	return state, codeVerifier, nonce
}

// Consume retrieves and deletes the entry for a state. Returns nil if expired/missing.
func (s *OIDCStateStore) Consume(state string) *OIDCStateEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purge()
	entry, ok := s.store[state]
	if !ok {
		return nil
	}
	delete(s.store, state)
	return entry
}

// purge removes expired entries. Called under lock.
func (s *OIDCStateStore) purge() {
	cutoff := time.Now().Add(-s.ttl)
	for k, v := range s.store {
		if v.CreatedAt.Before(cutoff) {
			delete(s.store, k)
		}
	}
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
