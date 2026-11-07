package washsale

import (
	"testing"
	"time"
)

func TestCalendarWindow(t *testing.T) {
	for _, test := range []struct {
		start, end string
		days       int
	}{
		{"2024-01-01T16:00:00-07:00", "2024-01-31T23:59:59-07:00", 30},
		{"2024-01-01T16:00:00-07:00", "2024-02-01T09:00:00-07:00", 31},
		{"2024-02-01T09:00:00-07:00", "2024-01-01T16:00:00-07:00", -31},
		{"2024-03-01T23:00:00-07:00", "2024-03-31T01:00:00-06:00", 30},
		{"2024-10-15T09:00:00-06:00", "2024-11-14T16:00:00-07:00", 30},
		{"2024-02-01T00:30:00+09:00", "2024-02-01T23:30:00-07:00", 0},
	} {
		t.Run(test.start+"/"+test.end, func(t *testing.T) {
			start, _ := time.Parse(time.RFC3339, test.start)
			end, _ := time.Parse(time.RFC3339, test.end)
			if got := DaysBetween(start, end); got != test.days {
				t.Fatalf("days = %d, want %d", got, test.days)
			}
		})
	}
}
