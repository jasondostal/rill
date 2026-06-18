package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Lexical dedup --------------------------------------------------------------
//
// rill keys entities by `type:slug`, so planUpsertEntity's exact-slug check
// already folds a re-declared name into the existing entity. What it does NOT
// catch is an *alternate surface form* of the same thing — "Acme CU" vs "Acme
// Communities Credit Union". Those slugify differently, and (being short
// strings) frequently fall outside the vector-similarity threshold, so a brand
// new duplicate entity gets minted silently. That's the recurring maintenance
// burden rill exists to avoid.
//
// This module adds a lexical backstop on the new-entity path. It does two
// things:
//
//  1. Alias-exact resolution: if the new name slugifies to an existing
//     same-type entity's *alias*, resolve to that entity (handled in
//     planUpsertEntity using fetchTypePeers) instead of creating a new one.
//
//  2. Variant soft-block: if the new name shares a *distinctive* token with an
//     existing same-type entity but is NOT in a subset/specialization
//     relationship with it, treat it as a likely alternate form and reject the
//     write (overridable via force_new).
//
// The subset exception is the crux. Without it, every specialization sibling in
// the graph would false-positive: "Azure" vs "Azure DevOps", "Kimi" vs "Kimi
// K2.6", "Rill" vs "Rill Sidecar". In each of those the longer name is the
// shorter name's tokens PLUS an extra distinctive token — a proper superset —
// which we read as "a more specific thing," not a duplicate. "Acme CU" vs "Acme
// Communities Credit Union" is different: neither token set contains the other
// ("cu" isn't in the long form), so it reads as a variant and blocks.

// lexDFMax is the document-frequency ceiling for a token to count as
// "distinctive" within an entity type: a token appearing in at most this many
// existing entities of the type is discriminating enough to anchor a
// variant match. Tokens that are everywhere in the type carry no signal.
const lexDFMax = 2

// lexStopwords are generic tokens that carry no disambiguating signal. Kept
// deliberately tiny — domain words like "credit", "union", "cu" ARE meaningful
// and must survive tokenization.
var lexStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true,
	"inc": true, "llc": true, "co": true, "corp": true, "ltd": true,
}

// entityTokens returns the distinct, lowercased, ≥2-char alphanumeric tokens of
// an entity's name + aliases, minus lexStopwords.
func entityTokens(name string, aliases []string) map[string]bool {
	out := map[string]bool{}
	addTokens(out, name)
	for _, a := range aliases {
		addTokens(out, a)
	}
	return out
}

// addTokens splits s on any non-alphanumeric boundary and adds the qualifying
// tokens to set. It keeps ≥2-char tokens and purely-numeric tokens of any
// length — version/sequence digits ("v0.6" vs "v0.7") are discriminating and
// must survive, or distinct versions collapse to the same token set.
func addTokens(set map[string]bool, s string) {
	for _, tok := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if lexStopwords[tok] {
			continue
		}
		if len(tok) < 2 && !isAllDigits(tok) {
			continue
		}
		set[tok] = true
	}
}

// isAllDigits reports whether s is non-empty and entirely ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// numericVersionSiblings reports whether a and b are distinct numbered variants
// of the same family — each has a token the other lacks, and at least one such
// private token is numeric (e.g. {v0,6} vs {v0,7}; {claude,3} vs {claude,4}).
// These are different things sharing a stem, never an alternate form, so they
// must not be blocked.
func numericVersionSiblings(a, b map[string]bool) bool {
	var aPriv, bPriv []string
	for t := range a {
		if !b[t] {
			aPriv = append(aPriv, t)
		}
	}
	for t := range b {
		if !a[t] {
			bPriv = append(bPriv, t)
		}
	}
	if len(aPriv) == 0 || len(bPriv) == 0 {
		return false
	}
	return anyNumeric(aPriv) || anyNumeric(bPriv)
}

func anyNumeric(toks []string) bool {
	for _, t := range toks {
		if isAllDigits(t) {
			return true
		}
	}
	return false
}

// properSubset reports whether a ⊊ b: every token of a is in b, and b has at
// least one token a lacks. Used to recognize specialization siblings.
func properSubset(a, b map[string]bool) bool {
	if len(a) == 0 || len(a) >= len(b) {
		return false
	}
	for tok := range a {
		if !b[tok] {
			return false
		}
	}
	return true
}

// peerEntity is the lightweight projection of an existing same-type entity used
// for lexical dedup: identity + the surface forms we tokenize.
type peerEntity struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	tokens  map[string]bool
}

// fetchTypePeers returns all non-merged entities of a type with their name +
// aliases, tokenized. One cheap SELECT, only run on the new-entity path.
func (s *Store) fetchTypePeers(ctx context.Context, t EntityType) ([]peerEntity, error) {
	stmt := fmt.Sprintf(`SELECT id, name, aliases FROM %s WHERE merged_into IS NONE;`, t)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []peerEntity
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].tokens = entityTokens(rows[i].Name, rows[i].Aliases)
	}
	return rows, nil
}

// lexicalDupCandidate scans peers for a likely VARIANT (alternate surface form)
// of a new entity with the given tokens. A peer qualifies when it shares at
// least one token that is distinctive within the type (document frequency
// ≤ dfMax across peers) AND is not in a subset/specialization relationship with
// the new entity in either direction. Returns the best candidate (most
// distinctive shared tokens) or nil when there's no variant to block on.
func lexicalDupCandidate(newTokens map[string]bool, peers []peerEntity, dfMax int) *peerEntity {
	if len(newTokens) == 0 || len(peers) == 0 {
		return nil
	}
	// Document frequency of each token across existing peers.
	df := map[string]int{}
	for i := range peers {
		for tok := range peers[i].tokens {
			df[tok]++
		}
	}
	var best *peerEntity
	bestScore := 0
	for i := range peers {
		p := &peers[i]
		// Specialization siblings (one token set a proper subset of the other)
		// are distinct things, not duplicates — never block on them.
		if properSubset(newTokens, p.tokens) || properSubset(p.tokens, newTokens) {
			continue
		}
		// Numbered siblings (v0.6 vs v0.7) are distinct versions, not variants.
		if numericVersionSiblings(newTokens, p.tokens) {
			continue
		}
		score := 0
		for tok := range newTokens {
			if p.tokens[tok] && df[tok] <= dfMax {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = p
		}
	}
	return best
}
