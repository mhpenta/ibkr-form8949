package report

import (
	"fmt"

	"github.com/mhpenta/ibkr-form8949/internal/types"
)

// PrintQuarterlyAnalysis prints a quarterly breakdown of gains/losses
func PrintQuarterlyAnalysis(analysis types.TaxAnalysis) {
	fmt.Println("\n=== Calendar-Quarter Realized Gain/Loss ===")

	fmt.Println("\nQuarter | Transactions | Total Proceeds | Total Cost | Total Gain/(Loss) | Short-Term | Long-Term | Unknown Term")
	fmt.Println("-----------------------------------------------------------------------------------------------")

	yearlyTotal := 0.0
	yearlyShortTerm := 0.0
	yearlyLongTerm := 0.0

	for i := 0; i < 4; i++ {
		q := analysis.QuarterlyAnalysis[i]
		if q.TransactionCount == 0 {
			continue
		}

		fmt.Printf("   Q%d   |     %4d     | $%12.2f | $%10.2f | $%15.2f | $%10.2f | $%10.2f | $%10.2f\n",
			q.Quarter,
			q.TransactionCount,
			q.TotalProceeds,
			q.TotalCostBasis,
			q.TotalGainOrLoss,
			q.ShortTermGainLoss,
			q.LongTermGainLoss, q.UnknownTermGainLoss)

		yearlyTotal += q.TotalGainOrLoss
		yearlyShortTerm += q.ShortTermGainLoss
		yearlyLongTerm += q.LongTermGainLoss
	}

	fmt.Println("-----------------------------------------------------------------------------------------------")
	fmt.Printf("  TOTAL |            | $%12.2f | $%10.2f | $%15.2f | $%10.2f | $%10.2f | $%10.2f\n",
		analysis.TotalProceeds,
		analysis.TotalCostBasis,
		yearlyTotal,
		yearlyShortTerm,
		yearlyLongTerm, analysis.UnknownTermGainOrLoss)

	fmt.Println("\nCalendar quarters summarize performance; they are not the IRS estimated-tax payment periods.")
	fmt.Println("This report does not calculate estimated tax payments.")

	PrintDisclaimer()
}
