package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeEmbedder is a test double for the Embedder interface that records call
// counts and can simulate errors or a hung upstream.
type fakeEmbedder struct {
	embedCalls int
	batchCalls int
	vec        []float64
	err        error
	block      bool // block until ctx is cancelled (simulates a hung OpenRouter)
}

func (f *fakeEmbedder) Embed(ctx context.Context, _ string) ([]float64, error) {
	f.embedCalls++
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

func (f *fakeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	f.batchCalls++
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, len(texts))
	for i := range out {
		out[i] = f.vec
	}
	return out, nil
}

func (f *fakeEmbedder) Model() string { return "fake-embed" }

func TestEmbedForWrite_Success(t *testing.T) {
	f := &fakeEmbedder{vec: []float64{0.1, 0.2, 0.3}}
	s := &Store{embedder: f}
	got := s.embedForWrite(context.Background(), "hello")
	if len(got) != 3 {
		t.Fatalf("want the embedder's vector back, got %v", got)
	}
	if f.embedCalls != 1 {
		t.Fatalf("want exactly 1 embed call, got %d", f.embedCalls)
	}
}

func TestEmbedForWrite_NilEmbedder(t *testing.T) {
	s := &Store{embedder: nil}
	if got := s.embedForWrite(context.Background(), "hello"); got != nil {
		t.Fatalf("nil embedder must yield nil vector, got %v", got)
	}
}

// Best-effort: an embedder error must NOT propagate — the write proceeds
// without a vector.
func TestEmbedForWrite_BestEffortOnError(t *testing.T) {
	f := &fakeEmbedder{err: errors.New("openrouter 401")}
	s := &Store{embedder: f}
	if got := s.embedForWrite(context.Background(), "hello"); got != nil {
		t.Fatalf("error path must yield nil vector, got %v", got)
	}
}

// The spin-killer: a hung embedder must be bounded by writeEmbedTimeout and
// fast-fail to nil, never blocking the write indefinitely.
func TestEmbedForWrite_BoundedByTimeout(t *testing.T) {
	orig := writeEmbedTimeout
	writeEmbedTimeout = 50 * time.Millisecond
	defer func() { writeEmbedTimeout = orig }()

	f := &fakeEmbedder{block: true}
	s := &Store{embedder: f}

	start := time.Now()
	got := s.embedForWrite(context.Background(), "hello")
	elapsed := time.Since(start)

	if got != nil {
		t.Fatalf("hung embedder must yield nil, got %v", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("embedForWrite should fast-fail near the %s budget, took %s", writeEmbedTimeout, elapsed)
	}
	if f.embedCalls != 1 {
		t.Fatalf("want 1 embed attempt, got %d", f.embedCalls)
	}
}
