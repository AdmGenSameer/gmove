package tests

import (
	"testing"

	"github.com/samarcher/gmove/internal/scanner"
)

func TestParseSelection(t *testing.T) {
	total := 10

	// 1. Single index
	res, err := scanner.ParseSelection("3", total)
	if err != nil || len(res) != 1 || res[0] != 2 {
		t.Errorf("expected [2], got %v, err %v", res, err)
	}

	// 2. Comma separated
	res, err = scanner.ParseSelection("1, 3, 5", total)
	if err != nil || len(res) != 3 || res[0] != 0 || res[1] != 2 || res[2] != 4 {
		t.Errorf("expected [0, 2, 4], got %v, err %v", res, err)
	}

	// 3. Range
	res, err = scanner.ParseSelection("2-4", total)
	if err != nil || len(res) != 3 || res[0] != 1 || res[1] != 2 || res[2] != 3 {
		t.Errorf("expected [1, 2, 3], got %v, err %v", res, err)
	}

	// 4. Mixed
	res, err = scanner.ParseSelection("1, 3, 7-9", total)
	if err != nil || len(res) != 5 {
		t.Errorf("expected 5 items, got %d (%v)", len(res), res)
	}

	// 5. "all"
	res, err = scanner.ParseSelection("all", total)
	if err != nil || len(res) != 10 {
		t.Errorf("expected 10 items for 'all', got %d", len(res))
	}

	// 6. Out of range error
	if _, err := scanner.ParseSelection("15", total); err == nil {
		t.Errorf("expected out of range error for 15, got nil")
	}

	// 7. Invalid string
	if _, err := scanner.ParseSelection("foo", total); err == nil {
		t.Errorf("expected error for 'foo', got nil")
	}
}
