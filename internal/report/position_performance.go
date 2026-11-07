package report

import (
	"fmt"
	"math"
	"sort"

	"github.com/mhpenta/ibkr-form8949/internal/types"
)

// PrintPositionPerformanceReport prints a consolidated position performance analysis report
func PrintPositionPerformanceReport(analysis types.TaxAnalysis) {
	fmt.Println("\n=== CONSOLIDATED POSITION PERFORMANCE REPORT ===")
	fmt.Println("This report shows overall trading performance by security, ignoring lot sizes")

	// Create a map to consolidate positions by security name (without lot size)
	securityMap := make(map[string]*types.PositionSummary)

	// First pass: group positions by base security name
	for _, pos := range analysis.PositionAnalysis {
		baseName := ExtractBaseName(pos.SecurityName)

		// If this security hasn't been seen before, initialize it
		if _, exists := securityMap[baseName]; !exists {
			securityMap[baseName] = &types.PositionSummary{
				SecurityName: baseName,
			}
		}

		// Add this position's data to the consolidated summary
		summary := securityMap[baseName]
		summary.TransactionCount += pos.TransactionCount
		summary.TotalProceeds += pos.TotalProceeds
		summary.TotalCostBasis += pos.TotalCostBasis
		summary.TotalGainOrLoss += pos.TotalGainOrLoss
		summary.ShortTermGainLoss += pos.ShortTermGainLoss
		summary.LongTermGainLoss += pos.LongTermGainLoss
		summary.WinningTrades += pos.WinningTrades
		summary.LosingTrades += pos.LosingTrades
		// Accumulate gross amounts before deriving consolidated averages.
		summary.AvgWinAmount += pos.AvgWinAmount * float64(pos.WinningTrades)
		summary.AvgLossAmount += pos.AvgLossAmount * float64(pos.LosingTrades)
		summary.UnknownTermGainLoss += pos.UnknownTermGainLoss
	}

	// Convert map to slice for sorting and calculate derived metrics
	var consolidatedPositions []types.PositionSummary

	totalGrossProfit := 0.0
	totalGrossLoss := 0.0

	securities := make([]string, 0, len(securityMap))
	for name := range securityMap {
		securities = append(securities, name)
	}
	sort.Strings(securities)
	for _, name := range securities {
		pos := securityMap[name]
		// Calculate hit rate
		if pos.TransactionCount > 0 {
			pos.HitRate = float64(pos.WinningTrades) / float64(pos.TransactionCount) * 100
		}

		grossWins, grossLosses := pos.AvgWinAmount, pos.AvgLossAmount
		totalGrossProfit += grossWins
		totalGrossLoss += grossLosses
		if pos.WinningTrades > 0 {
			pos.AvgWinAmount = grossWins / float64(pos.WinningTrades)
		}
		if pos.LosingTrades > 0 {
			pos.AvgLossAmount = grossLosses / float64(pos.LosingTrades)
		}

		// Calculate profit factor
		grossProfit := pos.AvgWinAmount * float64(pos.WinningTrades)
		grossLoss := pos.AvgLossAmount * float64(pos.LosingTrades)

		if grossLoss > 0 {
			pos.ProfitFactor = grossProfit / grossLoss
		} else if grossProfit > 0 {
			pos.ProfitFactor = math.Inf(1)
		} else {
			pos.ProfitFactor = 0.0
		}

		consolidatedPositions = append(consolidatedPositions, *pos)
	}

	// Calculate overall statistics
	totalTrades := 0
	totalWinningTrades := 0
	totalLosingTrades := 0
	totalSecurities := len(consolidatedPositions)
	profitableSecurities := 0
	unprofitableSecurities := 0
	breakEvenSecurities := 0

	for _, pos := range consolidatedPositions {
		totalTrades += pos.TransactionCount
		totalWinningTrades += pos.WinningTrades
		totalLosingTrades += pos.LosingTrades

		if pos.TotalGainOrLoss > 0 {
			profitableSecurities++
		} else if pos.TotalGainOrLoss < 0 {
			unprofitableSecurities++
		} else {
			breakEvenSecurities++
		}
	}

	// Overall hit rate
	overallHitRate := 0.0
	if totalTrades > 0 {
		overallHitRate = float64(totalWinningTrades) / float64(totalTrades) * 100
	}

	// Overall profit factor
	overallProfitFactor := 0.0
	if totalGrossLoss > 0 {
		overallProfitFactor = totalGrossProfit / totalGrossLoss
	} else if totalGrossProfit > 0 {
		overallProfitFactor = math.Inf(1)
	}

	// Sort by profitability
	SortPositionsByGainLoss(consolidatedPositions)

	// Print summary statistics
	fmt.Printf("\nOVERALL TRADING PERFORMANCE SUMMARY\n")
	fmt.Printf("Total Securities Traded:  %d\n", totalSecurities)
	fmt.Printf("Profitable Securities:    %d (%.1f%%)\n", profitableSecurities, percent(profitableSecurities, totalSecurities))
	fmt.Printf("Unprofitable Securities:  %d (%.1f%%)\n", unprofitableSecurities, percent(unprofitableSecurities, totalSecurities))
	if breakEvenSecurities > 0 {
		fmt.Printf("Break-even Securities:   %d (%.1f%%)\n", breakEvenSecurities, percent(breakEvenSecurities, totalSecurities))
	}
	fmt.Printf("Total Trades:             %d\n", totalTrades)
	fmt.Printf("Winning Trades:           %d (%.2f%%)\n", totalWinningTrades, overallHitRate)
	fmt.Printf("Losing Trades:            %d (%.2f%%)\n", totalLosingTrades, percent(totalLosingTrades, totalTrades))
	fmt.Printf("Profit Factor:            %.2f\n", overallProfitFactor)
	fmt.Printf("Total Gain/Loss:          $%.2f\n", analysis.TotalGainOrLoss)
	fmt.Printf("  Short-Term Gain/Loss:   $%.2f\n", analysis.ShortTermGainOrLoss)
	fmt.Printf("  Long-Term Gain/Loss:    $%.2f\n", analysis.LongTermGainOrLoss)
	fmt.Printf("  Unknown-Term Gain/Loss: $%.2f\n", analysis.UnknownTermGainOrLoss)

	// Calculate average profit/loss per trade
	avgPLPerTrade := 0.0
	if totalTrades > 0 {
		avgPLPerTrade = analysis.TotalGainOrLoss / float64(totalTrades)
	}
	fmt.Printf("Avg P/L Per Trade:        $%.2f\n", avgPLPerTrade)

	// Print top performers
	fmt.Println("\nTOP PERFORMING SECURITIES (by total P/L)")
	fmt.Println("Security | Trades | Win Rate | Total P/L | P/L per Trade | Profit Factor")
	fmt.Println("----------------------------------------------------------------------")

	// Print top 10 most profitable positions
	count := 0
	for _, pos := range consolidatedPositions {
		if pos.TotalGainOrLoss > 0 && count < 10 {
			plPerTrade := 0.0
			if pos.TransactionCount > 0 {
				plPerTrade = pos.TotalGainOrLoss / float64(pos.TransactionCount)
			}

			fmt.Printf("%s | %d | %.1f%% | $%.2f | $%.2f | %.2f\n",
				pos.SecurityName,
				pos.TransactionCount,
				pos.HitRate,
				pos.TotalGainOrLoss,
				plPerTrade,
				pos.ProfitFactor)
			count++
		}
	}

	// Print worst performers
	fmt.Println("\nWORST PERFORMING SECURITIES (by total P/L)")
	fmt.Println("Security | Trades | Win Rate | Total P/L | P/L per Trade | Profit Factor")
	fmt.Println("----------------------------------------------------------------------")

	// Print bottom 10 least profitable positions
	count = 0
	for i := len(consolidatedPositions) - 1; i >= 0; i-- {
		pos := consolidatedPositions[i]
		if pos.TotalGainOrLoss < 0 && count < 10 {
			plPerTrade := 0.0
			if pos.TransactionCount > 0 {
				plPerTrade = pos.TotalGainOrLoss / float64(pos.TransactionCount)
			}

			fmt.Printf("%s | %d | %.1f%% | $%.2f | $%.2f | %.2f\n",
				pos.SecurityName,
				pos.TransactionCount,
				pos.HitRate,
				pos.TotalGainOrLoss,
				plPerTrade,
				pos.ProfitFactor)
			count++
		}
	}

	// Print most actively traded
	fmt.Println("\nMOST ACTIVELY TRADED SECURITIES")
	fmt.Println("Security | Trades | Win Rate | Total P/L | Avg Win | Avg Loss | P/L per Trade")
	fmt.Println("---------------------------------------------------------------------------")

	// Sort by number of trades
	SortPositionsByTradeCount(consolidatedPositions)

	// Print top 10 most active positions
	for i, pos := range consolidatedPositions {
		if i < 10 {
			plPerTrade := 0.0
			if pos.TransactionCount > 0 {
				plPerTrade = pos.TotalGainOrLoss / float64(pos.TransactionCount)
			}

			fmt.Printf("%s | %d | %.1f%% | $%.2f | $%.2f | $%.2f | $%.2f\n",
				pos.SecurityName,
				pos.TransactionCount,
				pos.HitRate,
				pos.TotalGainOrLoss,
				pos.AvgWinAmount,
				pos.AvgLossAmount,
				plPerTrade)
		}
	}

	// Print highest win rates (for positions with at least 3 trades)
	fmt.Println("\nHIGHEST WIN RATE SECURITIES (min 3 trades)")
	fmt.Println("Security | Trades | Win Rate | Total P/L | Profit Factor")
	fmt.Println("----------------------------------------------------")

	// Sort by win rate
	SortPositionsByWinRate(consolidatedPositions)

	// Print top 10 highest win rate positions
	count = 0
	for _, pos := range consolidatedPositions {
		if pos.TransactionCount >= 3 && count < 10 {
			fmt.Printf("%s | %d | %.1f%% | $%.2f | %.2f\n",
				pos.SecurityName,
				pos.TransactionCount,
				pos.HitRate,
				pos.TotalGainOrLoss,
				pos.ProfitFactor)
			count++
		}
	}

	// Print best profit factors (min 3 trades, profit factor > 1)
	fmt.Println("\nBEST RISK-ADJUSTED RETURNS (min 3 trades, by profit factor)")
	fmt.Println("Security | Trades | Win Rate | Total P/L | Profit Factor")
	fmt.Println("----------------------------------------------------")

	// Sort by profit factor
	SortPositionsByProfitFactor(consolidatedPositions)

	// Print top 10 best profit factor positions
	count = 0
	for _, pos := range consolidatedPositions {
		if pos.TransactionCount >= 3 && pos.ProfitFactor > 1.0 && count < 10 {
			fmt.Printf("%s | %d | %.1f%% | $%.2f | %.2f\n",
				pos.SecurityName,
				pos.TransactionCount,
				pos.HitRate,
				pos.TotalGainOrLoss,
				pos.ProfitFactor)
			count++
		}
	}

	// Print trading insights
	fmt.Println("\nTRADING PERFORMANCE INSIGHTS")
	if overallHitRate > 50 {
		fmt.Printf("✓ Overall win rate of %.1f%% indicates generally successful trading decisions\n", overallHitRate)
	} else {
		fmt.Printf("⚠ Overall win rate of %.1f%% suggests room for improvement in trading decisions\n", overallHitRate)
	}

	securitySuccessRate := percent(profitableSecurities, totalSecurities)
	if securitySuccessRate > 50 {
		fmt.Printf("✓ %.1f%% of securities traded were profitable\n", securitySuccessRate)
	} else {
		fmt.Printf("⚠ Only %.1f%% of securities traded were profitable\n", securitySuccessRate)
	}

	if overallProfitFactor > 1.5 {
		fmt.Printf("✓ Strong profit factor of %.2f indicates good risk management\n", overallProfitFactor)
	} else if overallProfitFactor > 1.0 {
		fmt.Printf("✓ Profit factor of %.2f shows positive risk-adjusted returns\n", overallProfitFactor)
	} else {
		fmt.Printf("⚠ Profit factor of %.2f indicates poor risk-adjusted returns\n", overallProfitFactor)
	}

	// Add avg P/L per trade insight
	if avgPLPerTrade > 0 {
		fmt.Printf("✓ Average P/L per trade: $%.2f\n", avgPLPerTrade)
	} else {
		fmt.Printf("⚠ Average P/L per trade: $%.2f\n", avgPLPerTrade)
	}

	fmt.Println("\nRECOMMENDED ACTIONS")
	fmt.Println("1. Focus on securities with highest profit factors (best risk-adjusted returns)")
	fmt.Println("2. Consider increasing position sizes for securities with highest win rates")
	fmt.Println("3. Analyze what's common among your profitable securities (sectors, volatility, etc.)")
	fmt.Println("4. Review your trading strategy for securities with the largest losses")
	fmt.Println("5. For frequently traded securities with negative returns, reconsider your strategy")

	PrintDisclaimer()
}
