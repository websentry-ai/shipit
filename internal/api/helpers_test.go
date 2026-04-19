package api

import (
	"math"
	"testing"
)

func TestIntPtrToInt32Ptr(t *testing.T) {
	if got := intPtrToInt32Ptr(nil); got != nil {
		t.Errorf("nil input should yield nil, got %v", got)
	}

	v := 42
	got := intPtrToInt32Ptr(&v)
	if got == nil || *got != 42 {
		t.Errorf("want 42, got %v", got)
	}

	neg := -1
	if got := intPtrToInt32Ptr(&neg); got != nil {
		t.Errorf("negative input should yield nil, got %v", *got)
	}

	big := math.MaxInt32 + 1
	if got := intPtrToInt32Ptr(&big); got != nil {
		t.Errorf("overflow input should yield nil, got %v", *got)
	}

	edge := math.MaxInt32
	if got := intPtrToInt32Ptr(&edge); got == nil || *got != math.MaxInt32 {
		t.Errorf("MaxInt32 should pass through, got %v", got)
	}
}
