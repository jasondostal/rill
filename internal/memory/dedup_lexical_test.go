package memory

import "testing"

func TestEntityTokens(t *testing.T) {
	cases := []struct {
		name    string
		ename   string
		aliases []string
		want    []string // expected present tokens
		absent  []string // expected absent tokens
	}{
		{"abbrev", "Acme CU", nil, []string{"acme", "cu"}, nil},
		{"longform", "Acme Communities Credit Union", nil,
			[]string{"acme", "communities", "credit", "union"}, []string{"cu"}},
		{"stopwords_dropped", "Bank of America", nil, []string{"bank", "america"}, []string{"of"}},
		{"single_letters_dropped_numbers_kept", "Mimo 2.5 Pro", nil, []string{"mimo", "pro", "2", "5"}, nil},
		{"version_digits_kept", "Kimi K2.6", nil, []string{"kimi", "k2", "6"}, nil},
		{"aliases_folded", "Acme CU", []string{"ACCU"}, []string{"acme", "cu", "accu"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := entityTokens(c.ename, c.aliases)
			for _, w := range c.want {
				if !got[w] {
					t.Errorf("entityTokens(%q,%v) missing token %q; got %v", c.ename, c.aliases, w, keys(got))
				}
			}
			for _, a := range c.absent {
				if got[a] {
					t.Errorf("entityTokens(%q,%v) should not contain token %q; got %v", c.ename, c.aliases, a, keys(got))
				}
			}
		})
	}
}

func TestProperSubset(t *testing.T) {
	set := func(toks ...string) map[string]bool {
		m := map[string]bool{}
		for _, t := range toks {
			m[t] = true
		}
		return m
	}
	cases := []struct {
		name string
		a, b map[string]bool
		want bool
	}{
		{"sibling_specialization", set("azure"), set("azure", "devops"), true},
		{"rill_sidecar", set("rill"), set("rill", "sidecar"), true},
		{"equal_not_proper", set("acme", "cu"), set("acme", "cu"), false},
		{"superset_is_not_subset", set("azure", "devops"), set("azure"), false},
		{"disjoint", set("atlas"), set("rill"), false},
		{"variant_not_subset", set("acme", "cu"), set("acme", "communities", "credit", "union"), false},
		{"empty_a", set(), set("x"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := properSubset(c.a, c.b); got != c.want {
				t.Errorf("properSubset(%v,%v) = %v, want %v", keys(c.a), keys(c.b), got, c.want)
			}
		})
	}
}

func TestLexicalDupCandidate(t *testing.T) {
	peer := func(id, name string, aliases ...string) peerEntity {
		return peerEntity{ID: id, Name: name, Aliases: aliases, tokens: entityTokens(name, aliases)}
	}
	tok := func(name string, aliases ...string) map[string]bool {
		return entityTokens(name, aliases)
	}

	t.Run("acme_abbreviation_blocks", func(t *testing.T) {
		peers := []peerEntity{peer("organization:acme_communities_credit_union", "Acme Communities Credit Union")}
		got := lexicalDupCandidate(tok("Acme CU"), peers, lexDFMax)
		if got == nil || got.ID != "organization:acme_communities_credit_union" {
			t.Fatalf("expected Acme CU to match the credit union, got %v", got)
		}
	})

	t.Run("azure_devops_sibling_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("tool:azure", "Azure")}
		if got := lexicalDupCandidate(tok("Azure DevOps"), peers, lexDFMax); got != nil {
			t.Fatalf("Azure DevOps is a specialization of Azure, should not block; got %v", got.Name)
		}
	})

	t.Run("new_short_of_existing_long_sibling_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("tool:azure_devops", "Azure DevOps")}
		if got := lexicalDupCandidate(tok("Azure"), peers, lexDFMax); got != nil {
			t.Fatalf("Azure is a subset of Azure DevOps, should not block; got %v", got.Name)
		}
	})

	t.Run("rill_sidecar_sibling_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("project:rill", "rill")}
		if got := lexicalDupCandidate(tok("Rill Sidecar"), peers, lexDFMax); got != nil {
			t.Fatalf("Rill Sidecar is a specialization of rill, should not block; got %v", got.Name)
		}
	})

	t.Run("kimi_version_sibling_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("tool:kimi", "Kimi")}
		if got := lexicalDupCandidate(tok("Kimi K2.6"), peers, lexDFMax); got != nil {
			t.Fatalf("Kimi K2.6 is a specialization of Kimi, should not block; got %v", got.Name)
		}
	})

	t.Run("version_concepts_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("concept:v0_6", "v0.6")}
		if got := lexicalDupCandidate(tok("v0.7"), peers, lexDFMax); got != nil {
			t.Fatalf("v0.7 vs v0.6 are distinct versions, should not block; got %v", got.Name)
		}
	})

	t.Run("numbered_family_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("tool:claude_3", "Claude 3")}
		if got := lexicalDupCandidate(tok("Claude 4"), peers, lexDFMax); got != nil {
			t.Fatalf("Claude 4 vs Claude 3 are distinct, should not block; got %v", got.Name)
		}
	})

	t.Run("disjoint_allowed", func(t *testing.T) {
		peers := []peerEntity{peer("project:rill", "rill"), peer("project:atlas", "Atlas")}
		if got := lexicalDupCandidate(tok("Zephyr"), peers, lexDFMax); got != nil {
			t.Fatalf("disjoint name should not block; got %v", got.Name)
		}
	})

	t.Run("common_token_not_distinctive_allowed", func(t *testing.T) {
		// "service" appears in 3 peers → df=3 > lexDFMax → not distinctive.
		peers := []peerEntity{
			peer("project:alpha_service", "Alpha Service"),
			peer("project:beta_service", "Beta Service"),
			peer("project:gamma_service", "Gamma Service"),
		}
		if got := lexicalDupCandidate(tok("Delta Service"), peers, lexDFMax); got != nil {
			t.Fatalf("shared common token should not block; got %v", got.Name)
		}
	})

	t.Run("alias_token_anchors_match", func(t *testing.T) {
		// New "MRM" shares distinctive token with a peer only via its alias "MRM".
		peers := []peerEntity{peer("project:mrm", "Member Relationship Management", "MRM")}
		// "MRM" tokens {mrm} vs peer {member, relationship, management, mrm}:
		// {mrm} ⊊ peer → subset → sibling → allowed (alias-exact path handles real reuse).
		if got := lexicalDupCandidate(tok("MRM"), peers, lexDFMax); got != nil {
			t.Fatalf("expected subset/sibling, got block %v", got.Name)
		}
	})

	t.Run("empty_inputs", func(t *testing.T) {
		if got := lexicalDupCandidate(tok("Anything"), nil, lexDFMax); got != nil {
			t.Fatalf("no peers should not block; got %v", got)
		}
		if got := lexicalDupCandidate(map[string]bool{}, []peerEntity{peer("x:y", "Y")}, lexDFMax); got != nil {
			t.Fatalf("empty tokens should not block; got %v", got)
		}
	})
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
