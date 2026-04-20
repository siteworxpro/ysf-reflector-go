package bridge

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MatchesCron reports whether t matches the standard 5-field cron expression expr.
// Field order: minute hour day-of-month month day-of-week
//
// Supported syntax per field:
//   - *       any value
//   - N       exact match
//   - N-M     inclusive range
//   - */N     step from minimum
//   - N-M/N   range with step
//   - N,M,… comma-separated list (any of the above as items)
func MatchesCron(expr string, t time.Time) (bool, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false, fmt.Errorf("cron: expected 5 fields, got %d in %q", len(fields), expr)
	}

	type check struct {
		field    string
		val, min, max int
	}
	checks := []check{
		{fields[0], t.Minute(), 0, 59},
		{fields[1], t.Hour(), 0, 23},
		{fields[2], t.Day(), 1, 31},
		{fields[3], int(t.Month()), 1, 12},
		{fields[4], int(t.Weekday()), 0, 6},
	}

	for _, c := range checks {
		ok, err := matchField(c.field, c.val, c.min, c.max)
		if err != nil {
			return false, fmt.Errorf("cron field %q: %w", c.field, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// matchField evaluates a single cron field token against val.
func matchField(field string, val, min, max int) (bool, error) {
	// Comma-separated list: match if any item matches.
	if strings.Contains(field, ",") {
		for _, item := range strings.Split(field, ",") {
			ok, err := matchField(strings.TrimSpace(item), val, min, max)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}

	// Optional step suffix: .../N
	step := 1
	if idx := strings.LastIndex(field, "/"); idx >= 0 {
		s, err := strconv.Atoi(field[idx+1:])
		if err != nil || s < 1 {
			return false, fmt.Errorf("invalid step %q", field[idx+1:])
		}
		step = s
		field = field[:idx]
	}

	// Wildcard, range, or exact value.
	var lo, hi int
	switch {
	case field == "*":
		lo, hi = min, max
	case strings.Contains(field, "-"):
		parts := strings.SplitN(field, "-", 2)
		var err error
		if lo, err = strconv.Atoi(parts[0]); err != nil {
			return false, fmt.Errorf("invalid range start %q", parts[0])
		}
		if hi, err = strconv.Atoi(parts[1]); err != nil {
			return false, fmt.Errorf("invalid range end %q", parts[1])
		}
	default:
		n, err := strconv.Atoi(field)
		if err != nil {
			return false, fmt.Errorf("invalid value %q", field)
		}
		lo, hi = n, n
	}

	for v := lo; v <= hi; v += step {
		if v == val {
			return true, nil
		}
	}
	return false, nil
}
