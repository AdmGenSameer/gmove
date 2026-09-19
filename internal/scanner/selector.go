package scanner

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrEmptySelection   = errors.New("no files selected")
	ErrIndexOutOfRange  = errors.New("index out of range")
	ErrInvalidSelection = errors.New("invalid selection syntax")
)

// ParseSelection parses expressions like "1", "1,3,5", "1-5", "1,3,7-10", "all".
// Returns 0-based slice of unique indices.
func ParseSelection(input string, totalItems int) ([]int, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, ErrEmptySelection
	}

	if strings.EqualFold(trimmed, "all") {
		indices := make([]int, totalItems)
		for i := range indices {
			indices[i] = i
		}
		return indices, nil
	}

	seen := make(map[int]bool)
	var result []int

	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("%w: '%s'", ErrInvalidSelection, part)
			}

			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("%w: '%s'", ErrInvalidSelection, part)
			}

			if start > end {
				start, end = end, start
			}

			for i := start; i <= end; i++ {
				if i < 1 || i > totalItems {
					return nil, fmt.Errorf("%w: index %d not between 1 and %d", ErrIndexOutOfRange, i, totalItems)
				}
				idx := i - 1
				if !seen[idx] {
					seen[idx] = true
					result = append(result, idx)
				}
			}
		} else {
			val, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%w: '%s'", ErrInvalidSelection, part)
			}
			if val < 1 || val > totalItems {
				return nil, fmt.Errorf("%w: index %d not between 1 and %d", ErrIndexOutOfRange, val, totalItems)
			}
			idx := val - 1
			if !seen[idx] {
				seen[idx] = true
				result = append(result, idx)
			}
		}
	}

	if len(result) == 0 {
		return nil, ErrEmptySelection
	}

	return result, nil
}
