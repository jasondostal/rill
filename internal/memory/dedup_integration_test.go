//go:build integration

package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The lexical variant block rejects an alternate surface form of an existing
// same-type entity, names the candidate, and is overridable via force_new.
func TestRemember_LexicalVariantBlock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "Ada works at Acme Communities Credit Union.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Acme Communities Credit Union", Type: EntityOrganization}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// "Acme CU" is an alternate form → blocked, user-facing, names the candidate.
	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Acme CU rolled out a new portal.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Acme CU", Type: EntityOrganization}},
	})
	if err == nil {
		t.Fatal("expected Acme CU to be blocked as a variant")
	}
	if !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("block should be user-facing (ErrInvalidPayload), got %v", err)
	}
	if !strings.Contains(err.Error(), "organization:acme_communities_credit_union") {
		t.Fatalf("block should name the candidate entity, got %v", err)
	}

	// force_new overrides → a distinct org is created.
	res, err := s.Remember(ctx, RememberPayload{
		Summary: "Acme CU rolled out a new portal.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Acme CU", Type: EntityOrganization, ForceNew: true}},
	})
	if err != nil {
		t.Fatalf("force_new should succeed: %v", err)
	}
	if len(res.Entities) != 1 || !res.Entities[0].Created {
		t.Fatalf("force_new should create a new entity, got %+v", res.Entities)
	}
}

// A name that matches an existing entity's alias folds into that entity instead
// of minting a duplicate — and force_new does NOT bypass exact-alias identity.
func TestRemember_AliasExactResolution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "Acme Communities Credit Union, aka ACCU.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Acme Communities Credit Union", Type: EntityOrganization, Aliases: []string{"ACCU"}}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := s.Remember(ctx, RememberPayload{
		Summary: "ACCU hit a milestone.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "ACCU", Type: EntityOrganization}},
	})
	if err != nil {
		t.Fatalf("alias resolution: %v", err)
	}
	if len(res.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(res.Entities))
	}
	if res.Entities[0].Created {
		t.Fatal("ACCU should resolve to the existing entity, not create a new one")
	}
	if res.Entities[0].ID != "organization:acme_communities_credit_union" {
		t.Fatalf("ACCU should resolve to the CU, got %s", res.Entities[0].ID)
	}
}

// Specialization and numbered siblings are allowed through (no block): the
// graph legitimately holds rill + Rill Sidecar, Kimi + Kimi K2.6, etc.
func TestRemember_SpecializationSiblingAllowed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "rill is the memory graph.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "rill", Type: EntityProject}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := s.Remember(ctx, RememberPayload{
		Summary: "Rill Sidecar is the macOS client.",
		Kind:    KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Rill Sidecar", Type: EntityProject}},
	})
	if err != nil {
		t.Fatalf("specialization sibling should be allowed: %v", err)
	}
	if len(res.Entities) != 1 || !res.Entities[0].Created {
		t.Fatalf("Rill Sidecar should be created as a distinct project, got %+v", res.Entities)
	}
}
