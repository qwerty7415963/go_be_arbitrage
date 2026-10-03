package collector

import (
	"testing"
	"time"
)

// FUND-U-01: store-on-change decision table.
func TestShouldStoreFunding(t *testing.T) {
	now := time.Now().UTC()
	hb := time.Hour
	cases := []struct {
		name    string
		hasLast bool
		last    string
		newRate string
		lastAt  time.Time
		want    bool
	}{
		{"first sighting", false, "", "0.0001", time.Time{}, true},
		{"same rate fresh heartbeat", true, "0.0001", "0.0001", now.Add(-time.Minute), false},
		{"changed rate", true, "0.0001", "0.0002", now.Add(-time.Minute), true},
		{"same rate stale heartbeat", true, "0.0001", "0.0001", now.Add(-2 * time.Hour), true},
		{"equal numerics different text", true, "0.00010", "0.0001", now.Add(-time.Minute), false},
		{"unparseable stores safe", true, "oops", "0.0001", now.Add(-time.Minute), true},
	}
	for _, tc := range cases {
		if got := shouldStoreFunding(tc.hasLast, tc.last, tc.newRate, tc.lastAt, now, hb); got != tc.want {
			t.Errorf("%s: want %v", tc.name, tc.want)
		}
	}
}
