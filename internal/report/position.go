package report

import (
	"fmt"

	"github.com/mhpenta/ibkr-form8949/internal/types"
)

// PrintPositionAnalysis prints a detailed position-by-position analysis
func PrintPositionAnalysis(analysis types.TaxAnalysis) {
	fmt.Println("\n=== POSITION PERFORMANCE ANALYSIS ===")
	fmt.Println("This report analyzes each position's performance sorted by profit/loss")

	// Count total position metrics
	totalTrades := 0
	totalWinningTrades := 0
	totalLosingTrades := 0

	for _, position := range analysis.PositionAnalysis {
		totalTrades += position.TransactionCount
		totalWinningTrades += position.WinningTrades
		totalLosingTrades += position.LosingTrades
	}

	// Calculate overall hit rate
	overallHitRate := 0.0
	if totalTrades > 0 {
		overallHitRate = float64(totalWinningTrades) / float64(totalTrades) * 100
	}

	// Print summary statistics
	fmt.Printf("\nOVERALL TRADING PERFORMANCE\n")
	fmt.Printf("Total Positions:          %d\n", len(analysis.PositionAnalysis))
	fmt.Printf("Total Trades:             %d\n", totalTrades)
	fmt.Printf("Winning Trades:           %d (%.2f%%)\n", totalWinningTrades, overallHitRate)
	fmt.Printf("Losing Trades:            %d (%.2f%%)\n", totalLosingTrades, percent(totalLosingTrades, totalTrades))
	fmt.Printf("Total Gain/Loss:          $%.2f\n", analysis.TotalGainOrLoss)
	fmt.Printf("  Short-Term Gain/Loss:   $%.2f\n", analysis.ShortTermGainOrLoss)
	fmt.Printf("  Long-Term Gain/Loss:    $%.2f\n", analysis.LongTermGainOrLoss)
	fmt.Printf("  Unknown-Term Gain/Loss: $%.2f\n", analysis.UnknownTermGainOrLoss)

	// Print top 10 most profitable positions
	fmt.Println("\nMOST PROFITABLE POSITIONS")
	fmt.Println("Security | Trades | Win Rate | Profit/Loss | Avg Win | Avg Loss | Profit Factor")
	fmt.Println("------------------------------------------------------------------------------")

	count := 0
	for _, position := range analysis.PositionAnalysis {
		if position.TotalGainOrLoss > 0 && count < 10 {
			fmt.Printf("%s | %d | %.1f%% | $%.2f | $%.2f | $%.2f | %.2f\n",
				position.SecurityName,
				position.TransactionCount,
				position.HitRate,
				position.TotalGainOrLoss,
				position.AvgWinAmount,
				position.AvgLossAmount,
				position.ProfitFactor)
			count++
		}
	}

	// Print bottom 10 least profitable positions
	fmt.Println("\nLEAST PROFITABLE POSITIONS")
	fmt.Println("Security | Trades | Win Rate | Profit/Loss | Avg Win | Avg Loss | Profit Factor")
	fmt.Println("------------------------------------------------------------------------------")

	// We need to reverse through the position list since they're already sorted by gain/loss
	count = 0
	for i := len(analysis.PositionAnalysis) - 1; i >= 0; i-- {
		position := analysis.PositionAnalysis[i]
		if position.TotalGainOrLoss < 0 && count < 10 {
			fmt.Printf("%s | %d | %.1f%% | $%.2f | $%.2f | $%.2f | %.2f\n",
				position.SecurityName,
				position.TransactionCount,
				position.HitRate,
				position.TotalGainOrLoss,
				position.AvgWinAmount,
				position.AvgLossAmount,
				position.ProfitFactor)
			count++
		}
	}

	// Print positions with perfect win rates (if more than 1 trade)
	fmt.Println("\nPERFECT POSITIONS (100% Win Rate with 2+ trades)")
	fmt.Println("Security | Trades | Profit/Loss | Avg Win")
	fmt.Println("------------------------------------------")

	hasPerfectPositions := false
	for _, position := range analysis.PositionAnalysis {
		if position.HitRate == 100 && position.TransactionCount >= 2 {
			hasPerfectPositions = true
			fmt.Printf("%s | %d | $%.2f | $%.2f\n",
				position.SecurityName,
				position.TransactionCount,
				position.TotalGainOrLoss,
				position.AvgWinAmount)
		}
	}

	if !hasPerfectPositions {
		fmt.Println("No positions with 100% win rate and multiple trades found.")
	}

	// Print analysis insights
	fmt.Println("\nTRADING PERFORMANCE INSIGHTS")
	if overallHitRate > 50 {
		fmt.Printf("✓ Positive win rate of %.1f%% indicates generally successful trading decisions\n", overallHitRate)
	} else {
		fmt.Printf("⚠ Win rate of %.1f%% suggests room for improvement in trading decisions\n", overallHitRate)
	}

	// Give feedback based on top/bottom securities
	fmt.Println("\nRECOMMENDED ACTIONS")
	fmt.Println("1. Focus on your most profitable securities and analyze what made those trades successful")
	fmt.Println("2. Review your least profitable securities to identify patterns of poor performance")
	fmt.Println("3. Consider how position sizing affected overall returns on both winners and losers")
	if overallHitRate < 50 {
		fmt.Println("4. Work on improving win rate or adjusting risk-reward ratio to compensate for lower win rate")
	}

	PrintDisclaimer()
}
