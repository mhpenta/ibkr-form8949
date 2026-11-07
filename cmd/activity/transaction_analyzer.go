package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

func printTransactionList(report *activity.ActivityReport, symbol string) {
	trades := report.Trades
	if symbol != "" {
		trades = report.TradesBySymbol[symbol]
	}
	if len(trades) == 0 {
		if symbol == "" {
			fmt.Println("No trades found in the supplied statement.")
		} else {
			fmt.Printf("No trades found for symbol: %s\n", symbol)
		}
		return
	}
	trades = append([]activity.Trade(nil), trades...)
	sort.SliceStable(trades, func(i, j int) bool { return trades[i].DateTime.Before(trades[j].DateTime) })
	fmt.Println("ALL TRANSACTIONS:")
	fmt.Println("Date/Time | Symbol | Type | Quantity | Currency | Price | Reported Realized P/L")
	for _, t := range trades {
		pl := "--"
		if !t.RealizedPLMissing {
			pl = strconv.FormatFloat(t.RealizedPL, 'f', 2, 64)
		}
		fmt.Printf("%s | %s | %s | %g | %s | %g | %s\n", tradeTimestamp(t), t.Symbol, tradeSide(t), math.Abs(t.Quantity), t.Currency, t.TradePrice, pl)
	}
}

func tradeTimestamp(t activity.Trade) string {
	if t.DateOnly {
		return t.DateTime.Format(time.DateOnly)
	}
	return t.DateTime.Format(time.DateTime + ".999999999")
}

func tradeSide(t activity.Trade) string {
	switch {
	case t.IsSale:
		return "SELL"
	case t.IsBuy:
		return "BUY"
	default:
		return "OTHER"
	}
}

// AnalyzeSpecificTransaction examines a specific problematic transaction for a given symbol and date
func AnalyzeSpecificTransaction(report *activity.ActivityReport, symbol string, targetDate time.Time, targetQuantity float64) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	defer activity.PrintWashSaleScope()
	report.PrintCoverage(targetDate)
	if !report.CoversDates(targetDate.AddDate(0, 0, -30), targetDate.AddDate(0, 0, 30)) {
		fmt.Println("The full 61-day window is not covered by this statement.")
	}
	// Look for transactions of the specified symbol
	trades, found := report.TradesBySymbol[symbol]
	if !found || len(trades) == 0 {
		fmt.Printf("No %s trades found in the activity report.\n", symbol)
		return
	}

	// Sort by date
	trades = append([]activity.Trade(nil), trades...)
	sort.SliceStable(trades, func(i, j int) bool {
		return trades[i].DateTime.Before(trades[j].DateTime)
	})

	// Filter for the specific date transactions
	var dateSales []activity.Trade
	var totalSaleQuantity float64
	var totalSaleProceeds float64
	var totalSaleBasis, totalSalePL float64

	for _, trade := range trades {
		// Check if this is a sale on the target date
		if trade.DateTime.Year() == targetDate.Year() &&
			trade.DateTime.Month() == targetDate.Month() &&
			trade.DateTime.Day() == targetDate.Day() &&
			trade.IsSale {
			dateSales = append(dateSales, trade)
			totalSaleQuantity += math.Abs(trade.Quantity)
			totalSaleProceeds += trade.Proceeds
			totalSaleBasis += math.Abs(trade.Basis)
			totalSalePL += trade.RealizedPL
		}
	}

	// Check purchases on either side of the sale date.
	var recentPurchases []activity.Trade

	// Calendar days include day -30, day +30, and the sale date itself.

	for _, trade := range trades {
		days := washsale.DaysBetween(targetDate, trade.DateTime)
		if trade.IsBuy && days >= -30 && days <= 30 {
			recentPurchases = append(recentPurchases, trade)
		}
	}

	// Print detailed analysis of transactions
	fmt.Printf("\n=== %s %s Transaction Analysis ===\n\n",
		symbol, targetDate.Format("January 2, 2006"))

	if len(dateSales) == 0 {
		fmt.Printf("No %s sales found on %s.\n", symbol, targetDate.Format("January 2, 2006"))
		return
	}

	// Summary of all sales on target date
	fmt.Printf("Total %s Sales on %s: %d transactions\n",
		symbol, targetDate.Format("January 2, 2006"), len(dateSales))
	fmt.Printf("Total Quantity Sold: %.0f shares\n", totalSaleQuantity)
	fmt.Printf("Total Proceeds: $%.2f\n", totalSaleProceeds)
	fmt.Printf("Total Basis: $%.2f\n", totalSaleBasis)
	fmt.Printf("Total Reported Realized P/L: $%.2f\n\n", totalSalePL)
	fmt.Println("Reported P/L includes commissions and wash-sale adjustments; purchases below are candidates, not validated replacement lots.")

	// Individual transactions
	fmt.Println("INDIVIDUAL TRANSACTIONS:")
	fmt.Println("Timestamp | Quantity | Price | Proceeds | Basis | P/L")
	fmt.Println("-----------------------------------------------------")

	for _, sale := range dateSales {
		fmt.Printf("%s | %.0f | $%.2f | $%.2f | $%.2f | $%.2f\n",
			sale.DateTime.Format("15:04:05"),
			math.Abs(sale.Quantity),
			sale.TradePrice,
			sale.Proceeds,
			math.Abs(sale.Basis),
			sale.RealizedPL)
	}

	// Display candidate purchases in the inclusive calendar-day window.
	fmt.Printf("\nPURCHASES WITHIN 30 DAYS BEFORE OR AFTER SALE (INCLUDING SAME DAY):\n")
	if len(recentPurchases) == 0 {
		fmt.Printf("No purchases found in the supplied ±30-day window around %s.\n",
			targetDate.Format("January 2, 2006"))
	} else {
		fmt.Println("Date | Quantity | Price | Basis")
		fmt.Println("-----------------------------")
		for _, purchase := range recentPurchases {
			fmt.Printf("%s | %.0f | $%.2f | $%.2f\n",
				purchase.DateTime.Format("01/02/2006"),
				purchase.Quantity,
				purchase.TradePrice,
				purchase.Basis)
		}
	}

	// Try to match specific transaction quantity if provided
	if targetQuantity > 0 {
		fmt.Printf("\nMATCHING THE %.0f SHARE TRANSACTION:\n", targetQuantity)
		var matchedTransactions []activity.Trade
		for _, sale := range dateSales {
			if math.Abs(sale.Quantity) > 0.9*targetQuantity && math.Abs(sale.Quantity) < 1.1*targetQuantity {
				matchedTransactions = append(matchedTransactions, sale)
			}
		}

		if len(matchedTransactions) == 0 {
			fmt.Printf("No single transaction of approximately %.0f shares found.\n", targetQuantity)
			fmt.Println("The transaction may be split across multiple smaller orders.")

			// Group consecutive trades
			fmt.Println("\nPOTENTIAL GROUPING OF TRANSACTIONS:")

			// Sort by time
			sort.Slice(dateSales, func(i, j int) bool {
				return dateSales[i].DateTime.Before(dateSales[j].DateTime)
			})

			// Try to find combinations that add up close to target
			fmt.Printf("Attempting to find combinations of trades that add up to ~%.0f shares...\n", targetQuantity)

			// Try to find the best combination
			var bestCombination []activity.Trade
			var bestTotal float64

			// Try a sliding window approach
			for i := 0; i < len(dateSales); i++ {
				var combination []activity.Trade
				total := 0.0

				for j := i; j < len(dateSales); j++ {
					combination = append(combination, dateSales[j])
					total += math.Abs(dateSales[j].Quantity)

					// Check if this is better than our previous best
					if len(bestCombination) == 0 || math.Abs(total-targetQuantity) < math.Abs(bestTotal-targetQuantity) {
						bestCombination = append([]activity.Trade{}, combination...)
						bestTotal = total
					}

					// If we're very close or over, break
					if total >= targetQuantity {
						break
					}
				}
			}

			if len(bestCombination) > 0 {
				fmt.Printf("Best matching combination: %.0f shares (vs. %.0f target)\n", bestTotal, targetQuantity)
				fmt.Println("Transactions in this group:")

				total := 0.0
				totalProceeds := 0.0
				totalBasis := 0.0
				totalPL := 0.0

				for _, t := range bestCombination {
					fmt.Printf("  %s: %.0f shares @ $%.2f (P/L: $%.2f)\n",
						t.DateTime.Format("15:04:05"),
						math.Abs(t.Quantity),
						t.TradePrice,
						t.RealizedPL)
					total += math.Abs(t.Quantity)
					totalProceeds += t.Proceeds
					totalBasis += math.Abs(t.Basis)
					totalPL += t.RealizedPL
				}

				fmt.Printf("\nCombined Statistics:\n")
				fmt.Printf("Total Shares: %.0f\n", total)
				fmt.Printf("Total Proceeds: $%.2f\n", totalProceeds)
				fmt.Printf("Total Basis: $%.2f\n", totalBasis)
				fmt.Printf("Total Reported Realized P/L: $%.2f\n", totalPL)
			}
		} else {
			fmt.Printf("Found matching transaction(s):\n")
			for _, sale := range matchedTransactions {
				fmt.Printf("%s | %.0f shares | $%.2f | P/L: $%.2f\n",
					sale.DateTime.Format("01/02/2006 15:04:05"),
					math.Abs(sale.Quantity),
					sale.TradePrice,
					sale.RealizedPL)
			}
		}
	}

	// Summary and recommendations
	fmt.Printf("\nANALYSIS SUMMARY:\n")
	fmt.Println("1. Analyzed all transactions for the specified date and symbol")
	fmt.Println("2. Checked supplied purchases within 30 calendar days before and after the sale date")
	fmt.Println("3. Looked for specific transaction patterns that might explain tax treatment")

	fmt.Printf("\nRECOMMENDATIONS:\n")
	fmt.Println("1. Contact your broker for clarification about any discrepancies between transactions and tax reporting")
	fmt.Println("2. Ask for documentation on the specific lots used for wash sale determination")
	fmt.Println("3. Check if there were purchases in other accounts that might affect wash sale treatment")
	fmt.Println("4. Consider consulting a tax professional about potentially disputing any questionable wash sale designations")
}
