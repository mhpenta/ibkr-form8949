package report

import (
	"fmt"
	"sort"

	"github.com/mhpenta/ibkr-form8949/internal/types"
)

// PrintNoRepurchaseReport generates a detailed report focusing only on wash sales with no matching repurchase
func PrintNoRepurchaseReport(analysis types.TaxAnalysis) {
	// Filter to get only suspicious wash sales without nearby purchases
	var noRepurchaseWashSales []types.Transaction
	var totalAdjustment float64
	var totalGainLoss float64

	// Count categories
	zeroLossCount := 0
	variousLotCount := 0
	profitableWashCount := 0
	lossWashCount := 0
	barelyNegativeCount := 0 // Loss is very small (< $10)

	for _, tx := range analysis.SuspiciousWashSales {
		if tx.SuspiciousReason == "No matching purchase within reasonable timeframe" {
			noRepurchaseWashSales = append(noRepurchaseWashSales, tx)
			totalAdjustment += tx.Adjustment
			totalGainLoss += tx.GainOrLoss

			// Count categories
			if tx.UnadjustedGainOrLoss() == 0 {
				zeroLossCount++
			} else if tx.HasVariousLot && tx.UnadjustedGainOrLoss() < 0 {
				variousLotCount++
			} else if tx.UnadjustedGainOrLoss() > 0 {
				profitableWashCount++
			} else if tx.UnadjustedGainOrLoss() < 0 && tx.UnadjustedGainOrLoss() > -10 {
				barelyNegativeCount++
			} else if tx.UnadjustedGainOrLoss() < 0 {
				lossWashCount++
			}
		}
	}

	// Group by security
	securityMap := make(map[string][]types.Transaction)
	securityAdjustments := make(map[string]float64)
	securityCounts := make(map[string]int)

	for _, tx := range noRepurchaseWashSales {
		securityMap[tx.Description] = append(securityMap[tx.Description], tx)
		securityAdjustments[tx.Description] += tx.Adjustment
		securityCounts[tx.Description]++
	}

	// Create sorted securities array
	var securitiesSorted []SecuritySummary
	for name, txs := range securityMap {
		securitiesSorted = append(securitiesSorted, SecuritySummary{
			Name:         name,
			Adjustment:   securityAdjustments[name],
			Count:        securityCounts[name],
			Transactions: txs,
		})
	}

	// Sort securities by adjustment amount (largest first)
	sortSecurities(securitiesSorted)

	// Print the report
	fmt.Printf("\n=== WASH SALES WITH NO MATCHING PURCHASE ACTIVITY REPORT ===\n\n")
	fmt.Printf("Total transactions: %d\n", len(noRepurchaseWashSales))
	fmt.Printf("Total adjustment amount: $%.2f\n", totalAdjustment)
	fmt.Printf("Total gain/loss amount: $%.2f\n", totalGainLoss)
	fmt.Printf("Hypothetical gain/loss if these adjustments were removed: $%.2f\n\n", totalGainLoss-totalAdjustment)

	// Print categorical breakdown
	fmt.Println("TRANSACTION CATEGORIES:")
	fmt.Printf("Zero Before-Adjustment Gain/Loss: %d (review adjustment details)\n", zeroLossCount)
	fmt.Printf("VARIOUS Lot Transactions: %d (wash sale validation difficult)\n", variousLotCount)
	fmt.Printf("Profitable Before Adjustments: %d (review adjustment details)\n", profitableWashCount)
	fmt.Printf("Before-Adjustment Loss Under $10: %d\n", barelyNegativeCount)
	fmt.Printf("Before-Adjustment Loss Transactions: %d\n\n", lossWashCount)

	fmt.Println("TOP SECURITIES BY ADJUSTMENT AMOUNT:")
	fmt.Println("Security | Transaction Count | Total Adjustment")
	fmt.Println("--------------------------------------------------")

	for i, sec := range securitiesSorted {
		if i < 15 { // Show top 15 securities
			fmt.Printf("%s | %d | $%.2f\n",
				sec.Name,
				sec.Count,
				sec.Adjustment)
		}
	}

	fmt.Println("\nDETAILED BREAKDOWN BY SECURITY:")

	for _, sec := range securitiesSorted {
		fmt.Printf("\n=== %s (%d transactions, $%.2f total adjustment) ===\n",
			sec.Name, sec.Count, sec.Adjustment)

		fmt.Println("Date Sold | Adjustment | Adjusted Gain/Loss | Zero Before Adjustments | Various")
		fmt.Println("--------------------------------------------------")

		for _, tx := range sec.Transactions {
			zeroLoss := ""
			if tx.UnadjustedGainOrLoss() == 0 {
				zeroLoss = "Yes"
			}

			various := ""
			if tx.HasVariousLot {
				various = "Yes"
			}

			fmt.Printf("%s | $%.2f | $%.2f | %s | %s\n",
				tx.DateSold.Format("01/02/2006"),
				tx.Adjustment,
				tx.GainOrLoss,
				zeroLoss,
				various)
		}
	}

	fmt.Println("\n=== ANALYSIS NOTES ===")
	fmt.Println("These transactions are marked as wash sales in your brokerage data, but no matching purchase was found")
	fmt.Println("within the extended window in the supplied disposed lots. This could mean:")
	fmt.Println("1. Replacement shares are still held, or were disposed of outside the supplied reporting period")
	fmt.Println("2. The purchase may have been made by a related party (spouse, etc.)")
	fmt.Println("3. The transaction might be incorrectly marked as a wash sale")
	fmt.Println("4. The reporting system identified non-standard wash sales (e.g., option assignments)")

	fmt.Println("\n=== RECOMMENDED ACTIONS ===")
	fmt.Println("1. Focus on HIGH-VALUE adjustments first: The top 5-10 securities account for most of the impact")
	fmt.Println("2. Look for patterns: Multiple transactions in the same security on the same day are suspicious")
	fmt.Println("3. Check other accounts: Consider if you have the same security in IRAs, spouse accounts, etc.")
	fmt.Println("4. Zero adjusted gain/loss is normal for fully disallowed losses; inspect the loss before adjustments")
	fmt.Println("5. For VARIOUS lots: These are aggregated transactions which may include some legitimate wash sales")
	fmt.Println("6. Contact your broker: For large adjustments, ask for clarification about why they were flagged")

	PrintDisclaimer()
}

// SecuritySummary holds security-specific summary information
type SecuritySummary struct {
	Name         string
	Adjustment   float64
	Count        int
	Transactions []types.Transaction
}

// Helper function for sorting securities by adjustment amount
func sortSecurities(securities []SecuritySummary) {
	sort.Slice(securities, func(i, j int) bool {
		if securities[i].Adjustment == securities[j].Adjustment {
			return securities[i].Name < securities[j].Name
		}
		return securities[i].Adjustment > securities[j].Adjustment
	})
}
