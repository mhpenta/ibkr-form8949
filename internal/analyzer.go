package internal

import (
	"fmt"
	"math"
	"sort"

	"github.com/mhpenta/ibkr-form8949/internal/types"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

// AnalyzeTransactions processes all transactions and identifies suspicious wash sales
func AnalyzeTransactions(transactions []types.Transaction, year int, extendedWindow int, maxWindow int) types.TaxAnalysis {
	analysis := types.TaxAnalysis{
		ExtendedWindow: extendedWindow,
		MaxWindow:      maxWindow,
	}

	// Initialize quarterly data
	for i := 0; i < 4; i++ {
		analysis.QuarterlyAnalysis[i].Quarter = i + 1
	}

	// Keep position totals scoped to the reporting year and purchase evidence
	// across every supplied year. Descriptions retain their original display text.
	securitiesMap := make(map[string][]types.Transaction)
	washSecurities := make(map[string][]types.Transaction)

	// Maps to track position-level analysis
	positionTotals := make(map[string]types.PositionSummary)

	// First pass: aggregate totals and group by security
	for _, tx := range transactions {
		identity, _ := washsale.Security(tx.Description)
		washSecurities[identity] = append(washSecurities[identity], tx)
		// Filter out transactions not in the specified year
		if tx.DateSold.Year() != year {
			continue
		}

		analysis.Transactions = append(analysis.Transactions, tx)
		analysis.TotalProceeds += tx.Proceeds
		analysis.TotalCostBasis += tx.CostBasis
		analysis.TotalAdjustment += tx.Adjustment
		analysis.TotalGainOrLoss += tx.GainOrLoss
		if tx.TermUnknown {
			analysis.UnknownTermGainOrLoss += tx.GainOrLoss
		} else if tx.IsLongTerm {
			analysis.LongTermGainOrLoss += tx.GainOrLoss
		} else {
			analysis.ShortTermGainOrLoss += tx.GainOrLoss
		}

		// Add to quarterly analysis
		qIdx := tx.Quarter - 1
		if qIdx >= 0 && qIdx < 4 {
			analysis.QuarterlyAnalysis[qIdx].TotalGainOrLoss += tx.GainOrLoss
			analysis.QuarterlyAnalysis[qIdx].TotalProceeds += tx.Proceeds
			analysis.QuarterlyAnalysis[qIdx].TotalCostBasis += tx.CostBasis
			analysis.QuarterlyAnalysis[qIdx].TransactionCount++

			if tx.TermUnknown {
				analysis.QuarterlyAnalysis[qIdx].UnknownTermGainLoss += tx.GainOrLoss
			} else if tx.IsLongTerm {
				analysis.QuarterlyAnalysis[qIdx].LongTermGainLoss += tx.GainOrLoss
			} else {
				analysis.QuarterlyAnalysis[qIdx].ShortTermGainLoss += tx.GainOrLoss
			}
		}

		if tx.IsWashSale {
			analysis.TotalWashSaleAdjust += tx.Adjustment
			analysis.WashSaleTransactions = append(analysis.WashSaleTransactions, tx)
		}

		position := positionTotals[identity]
		position.SecurityName = identity
		position.TransactionCount++
		position.TotalProceeds += tx.Proceeds
		position.TotalCostBasis += tx.CostBasis
		position.TotalGainOrLoss += tx.GainOrLoss

		if tx.TermUnknown {
			position.UnknownTermGainLoss += tx.GainOrLoss
		} else if tx.IsLongTerm {
			position.LongTermGainLoss += tx.GainOrLoss
		} else {
			position.ShortTermGainLoss += tx.GainOrLoss
		}
		if tx.GainOrLoss > 0 {
			position.WinningTrades++
		} else if tx.GainOrLoss < 0 {
			position.LosingTrades++
		}

		positionTotals[identity] = position

		securitiesMap[identity] = append(securitiesMap[identity], tx)
	}

	// Form 8949 contains disposed lots only. A match is purchase evidence, not
	// proof that the broker's disallowed quantity or amount is correct.
	for _, security := range washSecurities {
		for i, tx := range security {
			if tx.DateSold.Year() != year || !tx.IsWashSale {
				continue
			}
			foundPurchase := false
			hasMatchingRepurchase := false
			ambiguousPurchase := false
			closestDays := 0
			for j, other := range security {
				if i == j || other.HasVariousLot || other.DateAcquired.IsZero() {
					continue
				}
				// A lot disposed of before this sale cannot be a replacement
				// still held at the loss sale. Same-day lot ordering is unknown.
				if other.DateSold.Before(tx.DateSold) {
					continue
				}
				days := washsale.DaysBetween(tx.DateSold, other.DateAcquired)
				if washsale.DaysBetween(tx.DateSold, other.DateSold) == 0 && days >= -30 && days <= 30 {
					ambiguousPurchase = true
					continue
				}
				if !foundPurchase || abs(days) < abs(closestDays) {
					closestDays = days
					foundPurchase = true
				}
				if days >= -30 && days <= 30 {
					hasMatchingRepurchase = true
				}
			}

			if tx.Adjustment != 0 && !hasMatchingRepurchase {
				tx.CustomData = "No eligible acquisition date in supplied disposed lots"
				if foundPurchase {
					tx.CustomData = formatDays(closestDays)
				}
				switch {
				case ambiguousPurchase:
					tx.SuspiciousReason = "Same-day disposition makes replacement evidence uncertain"
					tx.CustomData = "Execution order and closed-lot details required"
				case foundPurchase && abs(closestDays) <= maxWindow && abs(closestDays) <= extendedWindow:
					tx.SuspiciousReason = "Just outside 30-day window"
				case foundPurchase && abs(closestDays) <= maxWindow:
					tx.SuspiciousReason = "Outside 30-day window, but within extended range"
				default:
					tx.SuspiciousReason = "No matching purchase within reasonable timeframe"
				}
			}

			// Column (h) already includes column (g). A fully disallowed
			// loss normally has zero adjusted gain; inspect proceeds - basis.
			// Combined adjustment codes need their own breakdown first.
			if tx.SuspiciousReason == "" && (tx.Codes == "W" || tx.Codes == "") {
				if tx.UnadjustedGainOrLoss() == 0 {
					tx.SuspiciousReason = "Zero loss wash sale"
				} else if tx.UnadjustedGainOrLoss() > 0 {
					tx.SuspiciousReason = "No loss before adjustments"
				}
			}
			if tx.SuspiciousReason != "" {
				analysis.SuspiciousWashSales = append(analysis.SuspiciousWashSales, tx)
			}
		}
	}

	// Process position-level summaries
	for _, position := range positionTotals {
		// Calculate hit rate and other metrics
		if position.TransactionCount > 0 {
			position.HitRate = float64(position.WinningTrades) / float64(position.TransactionCount) * 100
		}

		// Calculate average win and loss amounts
		if position.WinningTrades > 0 {
			grossProfit := 0.0
			for _, tx := range securitiesMap[position.SecurityName] {
				if tx.GainOrLoss > 0 {
					grossProfit += tx.GainOrLoss
				}
			}
			position.AvgWinAmount = grossProfit / float64(position.WinningTrades)
		}

		if position.LosingTrades > 0 {
			grossLoss := 0.0
			for _, tx := range securitiesMap[position.SecurityName] {
				if tx.GainOrLoss < 0 {
					grossLoss += tx.GainOrLoss * -1 // Convert to positive for calculation
				}
			}
			position.AvgLossAmount = grossLoss / float64(position.LosingTrades)
		}

		// Calculate profit factor
		totalGrossProfit := position.AvgWinAmount * float64(position.WinningTrades)
		totalGrossLoss := position.AvgLossAmount * float64(position.LosingTrades)

		if totalGrossLoss > 0 {
			position.ProfitFactor = totalGrossProfit / totalGrossLoss
		} else if totalGrossProfit > 0 {
			position.ProfitFactor = math.Inf(1)
		}

		// Add to position analysis
		analysis.PositionAnalysis = append(analysis.PositionAnalysis, position)
	}

	// Sort positions by total gain/loss
	sortPositionsByGainLoss(analysis.PositionAnalysis)

	sort.Slice(analysis.SuspiciousWashSales, func(i, j int) bool {
		a, b := analysis.SuspiciousWashSales[i], analysis.SuspiciousWashSales[j]
		if a.Description != b.Description {
			return a.Description < b.Description
		}
		if !a.DateSold.Equal(b.DateSold) {
			return a.DateSold.Before(b.DateSold)
		}
		if !a.DateAcquired.Equal(b.DateAcquired) {
			return a.DateAcquired.Before(b.DateAcquired)
		}
		return a.Adjustment < b.Adjustment
	})
	return analysis
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// formatDays formats the days between transactions
func formatDays(days int) string {
	return fmt.Sprintf("%d days", days)
}

// sortPositionsByGainLoss sorts positions by gain/loss (most profitable first)
func sortPositionsByGainLoss(positions []types.PositionSummary) {
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].TotalGainOrLoss == positions[j].TotalGainOrLoss {
			return positions[i].SecurityName < positions[j].SecurityName
		}
		return positions[i].TotalGainOrLoss > positions[j].TotalGainOrLoss
	})
}
