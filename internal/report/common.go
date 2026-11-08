package report

import (
	"fmt"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
	"sort"

	"github.com/mhpenta/ibkr-form8949/internal/form8949"
)

// Common utility functions used by multiple report types

// FormatDollars formats a float as a dollar amount
func FormatDollars(amount float64) string {
	return fmt.Sprintf("$%.2f", amount)
}

// FormatPercent formats a float as a percentage
func FormatPercent(percent float64) string {
	return fmt.Sprintf("%.1f%%", percent)
}

// ExtractBaseName uses the same identity as wash-sale and position grouping.
// Option expiration, strike and type remain distinct.
func ExtractBaseName(fullName string) string {
	name, _ := washsale.Security(fullName)
	return name
}

func sortPositions(positions []form8949.PositionSummary, score func(form8949.PositionSummary) float64) {
	sort.Slice(positions, func(i, j int) bool {
		a, b := score(positions[i]), score(positions[j])
		if a == b {
			return positions[i].SecurityName < positions[j].SecurityName
		}
		return a > b
	})
}
func SortPositionsByGainLoss(p []form8949.PositionSummary) {
	sortPositions(p, func(x form8949.PositionSummary) float64 { return x.TotalGainOrLoss })
}
func SortPositionsByTradeCount(p []form8949.PositionSummary) {
	sortPositions(p, func(x form8949.PositionSummary) float64 { return float64(x.TransactionCount) })
}
func SortPositionsByWinRate(p []form8949.PositionSummary) {
	sortPositions(p, func(x form8949.PositionSummary) float64 { return x.HitRate })
}
func SortPositionsByProfitFactor(p []form8949.PositionSummary) {
	sortPositions(p, func(x form8949.PositionSummary) float64 { return x.ProfitFactor })
}

func percent(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(count) / float64(total)
}

// PrintDisclaimer prints the standard disclaimer for all reports
func PrintDisclaimer() {
	fmt.Println("\nNOTE: This analysis is for informational purposes only.")
	fmt.Println("Form 8949 contains disposed lots, not a complete purchase history. Replacement shares still held,")
	fmt.Println("other accounts, unknown acquisition dates and substantially identical securities may be missing.")
	fmt.Println("A missing match does not establish a broker error; a match does not validate the disallowed amount.")
	fmt.Println("Please consult with a tax professional before making any tax decisions.")
}
