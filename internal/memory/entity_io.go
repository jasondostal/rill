package memory

import (
	"context"
	"encoding/json"
	"fmt"
)

// existingEntity is the projection we need when checking dedup / merge.
type existingEntity struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	Summary      string   `json:"summary"`
	MentionCount int      `json:"mention_count"`
	Promoted     bool     `json:"promoted"`
	HandNotes    string   `json:"hand_notes"`
	DerivedCard  string   `json:"derived_card"`
	IsMerged     bool     `json:"is_merged"`
	MergedInto   string   `json:"merged_into_id"`
}

func (s *Store) fetchEntity(ctx context.Context, recID string) (*existingEntity, error) {
	stmt := fmt.Sprintf(`SELECT id, name, aliases, summary, mention_count, promoted, hand_notes, derived_card, (merged_into IS NOT NONE) AS is_merged, merged_into.id AS merged_into_id FROM %s;`, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []existingEntity
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, fmt.Errorf("decode entity row: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// nearestEntity finds the closest existing entity (by cosine distance on the
// embedding vector) of the same type. Returns nil if none is within the
// threshold (or none exists at all).
type nearestResult struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Distance float64 `json:"dist"`
}

func (s *Store) nearestEntity(ctx context.Context, t EntityType, emb []float64, maxDist float64) (*nearestResult, error) {
	if emb == nil {
		return nil, nil
	}
	embJSON, err := json.Marshal(emb)
	if err != nil {
		return nil, err
	}
	stmt := fmt.Sprintf(`SELECT id, name, vector::distance::knn() AS dist
		FROM %s WHERE embedding <|3,100|> %s
		AND vector::distance::knn() <= %f
		ORDER BY dist;`,
		t, string(embJSON), maxDist)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []nearestResult
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// relateMention writes a `memory -> mentions -> entity` edge.
// mentionStmt builds the RELATE statement linking a memory to an entity it
// mentions. Returned as a string so Remember can batch it into its transaction.
func mentionStmt(memID, entityID string) string {
	return fmt.Sprintf(`RELATE %s -> mentions -> %s SET weight = 1.0, created_at = time::now();`,
		memID, entityID)
}

func (s *Store) relateMention(ctx context.Context, memID, entityID string) error {
	_, err := s.db.SQL(ctx, mentionStmt(memID, entityID), true)
	return err
}
