package report

import (
	"fmt"

	"github.com/mhpenta/ibkr-form8949/internal/form8949"
)

// PrintStandardAnalysis prints a standard analysis report
func PrintStandardAnalysis(analysis form8949.TaxAnalysis) {
	// Count VARIOUS lots
	variousCount := 0
	variousAdjustment := 0.0
	specificCount := 0
	specificAdjustment := 0.0

	for _, tx := range analysis.WashSaleTransactions {
		if tx.HasVariousLot {
			variousCount++
			variousAdjustment += tx.Adjustment
		} else {
			specificCount++
			specificAdjustment += tx.Adjustment
		}
	}

	fmt.Println("\n=== Form 8949 Analysis Summary ===")
	fmt.Printf("Total Proceeds:         $%12.2f\n", analysis.TotalProceeds)
	fmt.Printf("Total Cost Basis:       $%12.2f\n", analysis.TotalCostBasis)
	fmt.Printf("Total Adjustments:      $%12.2f\n", analysis.TotalAdjustment)
	fmt.Printf("Total Gain/(Loss):      $%12.2f\n", analysis.TotalGainOrLoss)
	fmt.Printf("  Short-Term:           $%12.2f\n", analysis.ShortTermGainOrLoss)
	fmt.Printf("  Long-Term:            $%12.2f\n", analysis.LongTermGainOrLoss)
	fmt.Printf("  Unknown Term:         $%12.2f\n", analysis.UnknownTermGainOrLoss)
	fmt.Printf("Total Wash Sale Adjust: $%12.2f\n", analysis.TotalWashSaleAdjust)
	fmt.Printf("Number of Wash Sales:   %d\n", len(analysis.WashSaleTransactions))
	fmt.Printf("  VARIOUS lot sales:    %d (adjustment: $%.2f)\n", variousCount, variousAdjustment)
	fmt.Printf("  Specific lot sales:   %d (adjustment: $%.2f)\n", specificCount, specificAdjustment)
	fmt.Printf("Suspicious Wash Sales:  %d\n", len(analysis.SuspiciousWashSales))

	if len(analysis.SuspiciousWashSales) > 0 {
		fmt.Println("\n=== Suspicious Wash Sales ===")
		fmt.Println("These wash-sale adjustments need review against complete acquisition and lot history:")
		fmt.Println("Description | Date Sold | Adjusted Gain/Loss | Adjustment | Reason | Details")
		fmt.Println("----------------------------------------------------------------------------------")

		// Count by reason category
		justOutsideWindow := 0
		extendedRangeCount := 0
		noNearbyPurchase := 0
		zeroLoss := 0
		noLoss := 0

		for _, tx := range analysis.SuspiciousWashSales {
			// Count by reason
			switch tx.SuspiciousReason {
			case "Just outside 30-day window":
				justOutsideWindow++
			case "Outside 30-day window, but within extended range":
				extendedRangeCount++
			case "No matching purchase within reasonable timeframe":
				noNearbyPurchase++
			case "Zero loss wash sale":
				zeroLoss++
			case "No loss before adjustments":
				noLoss++
			}

			// Print with more detailed information
			fmt.Printf("%s | %s | $%.2f | $%.2f | %s | %s\n",
				tx.Description,
				tx.DateSold.Format("01/02/2006"),
				tx.GainOrLoss,
				tx.Adjustment,
				tx.SuspiciousReason,
				tx.CustomData)
		}

		// Print summary counts
		fmt.Println("\n=== Suspicious Wash Sale Summary ===")
		fmt.Printf("Total suspicious wash sales: %d\n", len(analysis.SuspiciousWashSales))
		fmt.Printf("  Just outside 30-day window (31-%d days): %d\n", analysis.ExtendedWindow, justOutsideWindow)
		fmt.Printf("  Outside 30-day window but within extended range (%d-%d days): %d\n",
			analysis.ExtendedWindow+1, analysis.MaxWindow, extendedRangeCount)
		fmt.Printf("  No nearby purchase (beyond %d days): %d\n", analysis.MaxWindow, noNearbyPurchase)
		fmt.Printf("  Zero gain/loss before adjustments: %d\n", zeroLoss)
		fmt.Printf("  Gain before adjustments: %d\n", noLoss)
	}

	// Calculate what gain/loss would be without suspicious wash sales
	adjustedGainLoss := analysis.TotalGainOrLoss
	suspiciousAdjustments := 0.0
	for _, tx := range analysis.SuspiciousWashSales {
		suspiciousAdjustments += tx.Adjustment
	}

	adjustedGainLoss -= suspiciousAdjustments

	fmt.Println("\n=== Hypothetical Adjustment Review ===")
	fmt.Printf("Gain/(Loss) if flagged adjustments were removed: $%.2f\n", adjustedGainLoss)
	fmt.Printf("Flagged adjustments requiring verification:     $%.2f\n", suspiciousAdjustments)

	PrintDisclaimer()
}
