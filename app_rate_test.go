package main

import "testing"

// TestRateOf pins the per-direction rule: a negative delta is a core that
// restarted and counts from zero, and that direction reads as no traffic —
// never as a negative rate.
func TestRateOf(t *testing.T) {
	cases := []struct {
		name  string
		delta int64
		span  int64
		want  int64
	}{
		{"plain", 1000, 1000, 1000},
		{"negative delta (restarted core)", -5, 1000, 0},
		{"zero delta", 0, 1000, 0},
		{"zero span", 1000, 0, 0},
		{"sub-second span", 500, 250, 2000},
	}
	for _, c := range cases {
		if got := rateOf(c.delta, c.span); got != c.want {
			t.Errorf("%s: rateOf(%d, %d) = %d, want %d", c.name, c.delta, c.span, got, c.want)
		}
	}
}
