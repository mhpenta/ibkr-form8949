// Package washsale contains shared wash-sale inspection helpers.
package washsale

import "time"

// Day preserves the written calendar date, ignoring clock time and UTC offset.
// IBKR trade timestamps have no offset; converting them between zones would
// change the statement's trade date. UTC here is only a calendar arithmetic aid.
func Day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// DaysBetween returns the signed number of calendar days from start to end.
func DaysBetween(start, end time.Time) int {
	return int(Day(end).Sub(Day(start)) / (24 * time.Hour))
}

// RepurchaseDate is the first date outside this sale's 30-day forward window.
func RepurchaseDate(sale time.Time) time.Time {
	return Day(sale).AddDate(0, 0, 31)
}
