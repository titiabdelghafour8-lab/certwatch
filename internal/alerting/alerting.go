// Package alerting holds the pure decision logic: which status a monitor is in
// and which alert threshold, if any, is due. It has no I/O so it is trivially
// testable and shared by the worker and the CLI.
package alerting

import (
	"fmt"
	"sort"

	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
)

// CriticalDays is the remaining-days boundary below which a monitor is critical.
const CriticalDays = 7

// ExpiredThreshold is the threshold value recorded for "already expired" alerts.
const ExpiredThreshold = 0

// StatusFor maps remaining days to a status.
func StatusFor(daysLeft int, warnDays []int) model.Status {
	switch {
	case daysLeft < 0:
		return model.StatusExpired
	case daysLeft <= CriticalDays:
		return model.StatusCritical
	case daysLeft <= maxOf(warnDays):
		return model.StatusWarning
	default:
		return model.StatusOK
	}
}

// DueThreshold returns the most urgent threshold crossed by daysLeft, or -1
// when none is crossed. Only the most urgent one is reported so that adding a
// monitor that expires in 5 days produces one alert, not three.
func DueThreshold(daysLeft int, warnDays []int) int {
	if daysLeft < 0 {
		return ExpiredThreshold
	}
	best := -1
	for _, t := range warnDays {
		if daysLeft <= t && (best == -1 || t < best) {
			best = t
		}
	}
	return best
}

// NormalizeWarnDays validates thresholds and returns them deduplicated and
// sorted in descending order. Empty input yields the defaults.
func NormalizeWarnDays(in []int) ([]int, error) {
	if len(in) == 0 {
		return append([]int(nil), model.DefaultWarnDays...), nil
	}
	if len(in) > 10 {
		return nil, fmt.Errorf("at most 10 warn_days values are allowed")
	}
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, d := range in {
		if d < 1 || d > 365 {
			return nil, fmt.Errorf("warn_days values must be between 1 and 365, got %d", d)
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out, nil
}

func maxOf(xs []int) int {
	m := 0
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}
