package activity

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

func parsePeriod(period string) (time.Time, time.Time) {
	parts := strings.Split(strings.TrimSpace(period), " - ")
	parse := func(value string) time.Time {
		for _, layout := range []string{"January 2, 2006", time.DateOnly, "01/02/2006"} {
			if t, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
				return t
			}
		}
		return time.Time{}
	}
	if len(parts) == 1 {
		d := parse(parts[0])
		return d, d
	}
	if len(parts) != 2 {
		return time.Time{}, time.Time{}
	}
	start, end := parse(parts[0]), parse(parts[1])
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return time.Time{}, time.Time{}
	}
	return start, end
}

// CoversDates describes statement coverage, not completeness across accounts.
func (r *ActivityReport) CoversDates(start, end time.Time) bool {
	return !r.PeriodStart.IsZero() && !r.PeriodEnd.IsZero() &&
		!washsale.Day(start).Before(r.PeriodStart) && !washsale.Day(end).After(r.PeriodEnd)
}

func (r *ActivityReport) PrintCoverage(asOf time.Time) {
	if r.PeriodStart.IsZero() || r.PeriodEnd.IsZero() {
		fmt.Println("Statement coverage is unknown; absence of trades cannot establish a clear wash-sale window.")
		return
	}
	fmt.Printf("Statement coverage: %s through %s\n", r.PeriodStart.Format(time.DateOnly), r.PeriodEnd.Format(time.DateOnly))
	if !r.CoversDates(asOf, asOf) {
		fmt.Println("The comparison date is outside statement coverage; newer or earlier trades are unknown.")
	}
}

// PrintWindowCoverage also warns when the statement starts inside the 30 days
// before asOf: loss sales from those missing days could still have open windows.
func (r *ActivityReport) PrintWindowCoverage(asOf time.Time) {
	r.PrintCoverage(asOf)
	if r.PeriodStart.IsZero() || r.PeriodEnd.IsZero() {
		return
	}
	day := washsale.Day(asOf)
	lookback := day.AddDate(0, 0, -30)
	// An as-of date before the statement start is already reported above.
	if lookback.Before(r.PeriodStart) && !day.Before(r.PeriodStart) {
		fmt.Printf("The statement starts on %s; loss sales from %s through %s are not included and could still have open windows.\n",
			r.PeriodStart.Format(time.DateOnly), lookback.Format(time.DateOnly), r.PeriodStart.AddDate(0, 0, -1).Format(time.DateOnly))
	}
}

// A reconciled broker snapshot can establish a flat position after a trade.
// An opening quantity alone is not a dated acquisition or a tax lot.
func markKnownFlatPositions(r *ActivityReport) {
	bySymbol := make(map[string][]int)
	for i, t := range r.Trades {
		bySymbol[t.Symbol] = append(bySymbol[t.Symbol], i)
	}
	for symbol, indices := range bySymbol {
		rows := r.MarkToMarketPerformanceBySymbol[symbol]
		if len(rows) != 1 || len(indices) == 0 {
			continue
		}
		row := rows[0]
		if row.PriorQuantityMissing || row.CurrentQuantityMissing {
			continue
		}
		key := tradeInventoryKey(r.Trades[indices[0]])
		valid := row.AssetCategory == r.Trades[indices[0]].AssetCategory && row.AssetCategory != "Forex"
		balance := row.PriorQuantity
		for _, i := range indices {
			if tradeInventoryKey(r.Trades[i]) != key {
				valid = false
			}
			balance += r.Trades[i].Quantity
		}
		if !valid || math.Abs(balance-row.CurrentQuantity) > quantityEpsilon {
			continue
		}
		sort.SliceStable(indices, func(i, j int) bool { return r.Trades[indices[i]].DateTime.Before(r.Trades[indices[j]].DateTime) })
		// A date-only row can move across any timed trade on that day. Avoid
		// treating an intermediate zero in that arbitrary order as a checkpoint.
		ambiguousDays := make(map[time.Time]bool)
		for _, i := range indices {
			for _, j := range indices {
				if ambiguousTradeOrder(r.Trades[i], r.Trades[j]) {
					ambiguousDays[washsale.Day(r.Trades[i].DateTime)] = true
				}
			}
		}
		balance = row.PriorQuantity
		for _, i := range indices {
			balance += r.Trades[i].Quantity
			if math.Abs(balance) <= quantityEpsilon && !ambiguousDays[washsale.Day(r.Trades[i].DateTime)] {
				r.Trades[i].PositionKnownFlat = true
			}
		}
	}
}

func validateSummaryNumbers(records [][]string, lines []int) error {
	var header []string
	for i, row := range records {
		if len(row) < 2 {
			continue
		}
		if row[0] == "Change in NAV" && row[1] == "Data" {
			if len(row) < 4 {
				return fmt.Errorf("line %d: incomplete NAV row", lines[i])
			}
			if _, err := parseNumber(row[3]); err != nil {
				return fmt.Errorf("line %d: invalid NAV amount: %w", lines[i], err)
			}
		}
		if row[0] != "Mark-to-Market Performance Summary" {
			continue
		}
		if row[1] == "Header" {
			header = row
			fields, err := fieldIndexes(row)
			if err != nil {
				return fmt.Errorf("line %d: %w", lines[i], err)
			}
			for _, name := range []string{"Asset Category", "Symbol", "Prior Quantity", "Current Quantity", "Mark-to-Market P/L Total"} {
				if _, ok := fields[name]; !ok {
					return fmt.Errorf("line %d: missing MTM column %q", lines[i], name)
				}
			}
		}
		if row[1] != "Data" {
			continue
		}
		if header == nil {
			return fmt.Errorf("line %d: MTM data before header", lines[i])
		}
		for column, name := range header {
			if name == "Prior Quantity" || name == "Current Quantity" || name == "Prior Price" || name == "Current Price" || strings.HasPrefix(name, "Mark-to-Market P/L ") {
				if column >= len(row) {
					return fmt.Errorf("line %d: missing MTM value %q", lines[i], name)
				}
				if _, err := parseNumber(row[column]); err != nil {
					return fmt.Errorf("line %d: invalid %s: %w", lines[i], name, err)
				}
			}
		}
	}
	return nil
}
