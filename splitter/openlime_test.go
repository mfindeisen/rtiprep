package splitter

import "testing"

func TestExpandOpenLimeScalars(t *testing.T) {
	got := expandOpenLimeScalars([]float64{1, 2, 3}, 9)
	if len(got) != 9 {
		t.Fatalf("expected 9 values, got %d", len(got))
	}
	if got[0] != 1 || got[1] != 1 || got[2] != 1 || got[3] != 2 || got[8] != 3 {
		t.Fatalf("unexpected expansion: %v", got)
	}
}

func TestOpenLimePlaneCount(t *testing.T) {
	if got := openLimePlaneCount("HSH_RTI", 9); got != 27 {
		t.Fatalf("expected 27 planes, got %d", got)
	}
}
