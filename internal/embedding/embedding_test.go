package embedding

import (
	"testing"
)

func TestToFloat32(t *testing.T) {
	// []float64 — the most common path from Embed().
	f64 := []float64{1.0, 2.0, 3.0}
	out, ok := ToFloat32(f64)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(out) != 3 || out[0] != 1.0 || out[1] != 2.0 || out[2] != 3.0 {
		t.Fatalf("got %v", out)
	}

	// []float32 — pass-through (no re-alloc).
	f32 := []float32{4.0, 5.0}
	out, ok = ToFloat32(f32)
	if !ok {
		t.Fatal("expected ok for []float32 pass-through")
	}
	if len(out) != 2 || out[0] != 4.0 || out[1] != 5.0 {
		t.Fatalf("got %v", out)
	}
	// Verify it's the same slice (pass-through, not copy).
	if &out[0] != &f32[0] {
		t.Error("expected same backing array for []float32 pass-through")
	}

	// []any — the SurrealDB driver path.
	anySlice := []any{float64(7.0), float64(8.0)}
	out, ok = ToFloat32(anySlice)
	if !ok {
		t.Fatal("expected ok for []any")
	}
	if len(out) != 2 || out[0] != 7.0 || out[1] != 8.0 {
		t.Fatalf("got %v", out)
	}

	// []any with mixed types.
	anyMixed := []any{float32(9.0), float64(10.0)}
	out, ok = ToFloat32(anyMixed)
	if !ok {
		t.Fatal("expected ok for mixed []any")
	}
	if len(out) != 2 || out[0] != 9.0 || out[1] != 10.0 {
		t.Fatalf("got %v", out)
	}

	// Reject unknown types.
	out, ok = ToFloat32("not an embedding")
	if ok {
		t.Error("expected false for string")
	}
	if out != nil {
		t.Error("expected nil for invalid input")
	}

	// Reject []any with a bad element.
	badAny := []any{float64(1.0), "oops"}
	out, ok = ToFloat32(badAny)
	if ok {
		t.Error("expected false for []any with bad element")
	}
	if out != nil {
		t.Error("expected nil for invalid []any")
	}

	// Empty slice — should be ok.
	out, ok = ToFloat32([]float64{})
	if !ok {
		t.Fatal("expected ok for empty []float64")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty, got %v", out)
	}
}
