package activity

import (
	"fmt"
	"math"
	"sort"
)

// SymbolAnalysis represents a detailed analysis of trades for a single symbol
type SymbolAnalysis struct {
	Symbol           string
	AllTrades        []Trade
	BuyTrades        []Trade
	SellTrades       []Trade
	WashSales        []Trade
	SuspiciousTrades []WashSaleAnalysis
	WashSaleChecks   []WashSaleAnalysis
}

// WashSaleAnalysis represents the analysis of a suspicious wash sale
type WashSaleAnalysis struct {
	Trade             Trade
	NearestRepurchase *Trade
	DaysDifference    int
	MatchedQuantity   float64
	Reason            string
}

// AnalyzeSymbol performs a detailed analysis of trades for a single symbol
func AnalyzeSymbol(trades []Trade) SymbolAnalysis {
	analysis := SymbolAnalysis{
		AllTrades: trades,
	}

	if len(trades) > 0 {
		analysis.Symbol = trades[0].Symbol
	}

	// Separate buys and sells
	for _, trade := range trades {
		if trade.IsBuy {
			analysis.BuyTrades = append(analysis.BuyTrades, trade)
		} else if trade.IsSale {
			analysis.SellTrades = append(analysis.SellTrades, trade)
		}

		if trade.IsWashSale {
			analysis.WashSales = append(analysis.WashSales, trade)
		}
	}

	// Sort by date
	sort.SliceStable(analysis.BuyTrades, func(i, j int) bool {
		return analysis.BuyTrades[i].DateTime.Before(analysis.BuyTrades[j].DateTime)
	})

	sort.SliceStable(analysis.SellTrades, func(i, j int) bool {
		return analysis.SellTrades[i].DateTime.Before(analysis.SellTrades[j].DateTime)
	})

	sort.SliceStable(analysis.WashSales, func(i, j int) bool {
		return analysis.WashSales[i].DateTime.Before(analysis.WashSales[j].DateTime)
	})

	ordered := append([]Trade(nil), trades...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].DateTime.Before(ordered[j].DateTime)
	})
	checks := inspectReplacementLots(ordered)
	for i, sale := range ordered {
		if !sale.IsWashSale {
			continue
		}
		check, found := checks[i]
		if !found {
			check = WashSaleAnalysis{Trade: sale, Reason: "Short-position or opening-trade wash adjustment requires broker lot details"}
		}
		analysis.WashSaleChecks = append(analysis.WashSaleChecks, check)
		if check.Reason != "" {
			analysis.SuspiciousTrades = append(analysis.SuspiciousTrades, check)
		}
	}
	return analysis
}

// PrintSymbolAnalysis prints a detailed analysis for a specific symbol
func PrintSymbolAnalysis(analysis SymbolAnalysis) {
	fmt.Printf("\n=== %s Analysis ===\n", analysis.Symbol)
	fmt.Printf("Total Trades: %d (Buys: %d, Sells: %d)\n",
		len(analysis.AllTrades), len(analysis.BuyTrades), len(analysis.SellTrades))
	fmt.Printf("Wash Sales: %d\n", len(analysis.WashSales))
	fmt.Printf("Wash-Sale Adjustments Requiring Review: %d\n\n", len(analysis.SuspiciousTrades))
	fmt.Println("Replacement matching uses FIFO on supplied trades; reported P/L includes disallowed losses.")

	if len(analysis.SuspiciousTrades) > 0 {
		fmt.Println("SUSPICIOUS WASH SALE DETAILS:")
		fmt.Println("Date | Quantity | Price | Proceeds | Basis | P/L | Nearest Repurchase | Days Diff | Reason")
		fmt.Println("----------------------------------------------------------------------------")

		for _, suspicious := range analysis.SuspiciousTrades {
			sale := suspicious.Trade
			nearestInfo := "None found"
			if suspicious.NearestRepurchase != nil {
				nearestBuy := *suspicious.NearestRepurchase
				nearestInfo = fmt.Sprintf("%s (%.0f @ $%.2f)",
					nearestBuy.DateTime.Format("01/02/2006"),
					math.Abs(nearestBuy.Quantity),
					nearestBuy.TradePrice)
			}

			fmt.Printf("%s | %.0f | $%.2f | $%.2f | $%.2f | $%.2f | %s | %d days | %s\n",
				sale.DateTime.Format("01/02/2006"),
				math.Abs(sale.Quantity),
				sale.TradePrice,
				sale.Proceeds,
				math.Abs(sale.Basis),
				sale.RealizedPL,
				nearestInfo,
				suspicious.DaysDifference,
				suspicious.Reason)
		}

		fmt.Println("\nALL TRADES TIMELINE:")
		fmt.Println("Date | Type | Quantity | Price | P/L | Wash Sale Code")
		fmt.Println("----------------------------------------------------")

		// Combine all trades for timeline
		allTrades := append([]Trade{}, analysis.AllTrades...)
		sort.SliceStable(allTrades, func(i, j int) bool {
			return allTrades[i].DateTime.Before(allTrades[j].DateTime)
		})

		for _, trade := range allTrades {
			tradeType := "BUY"
			if trade.IsSale {
				tradeType = "SELL"
			}

			washSaleInfo := ""
			if trade.IsWashSale {
				washSaleInfo = "WASH"
			}

			fmt.Printf("%s | %s | %.0f | $%.2f | $%.2f | %s\n",
				trade.DateTime.Format("01/02/2006"),
				tradeType,
				math.Abs(trade.Quantity),
				trade.TradePrice,
				trade.RealizedPL,
				washSaleInfo)
		}
	} else if len(analysis.WashSales) > 0 {
		fmt.Println("Candidate replacement quantities were found using FIFO on the supplied trades.")
		fmt.Println("The broker's disallowed amounts still require closed-lot details to validate.")
	}
}

// PrintWashSaleReport prints a report of all suspicious wash sales across all symbols
func PrintWashSaleReport(report *ActivityReport) {
	// Collect all suspicious wash sales across all symbols
	allSuspicious := []struct {
		Symbol   string
		Analysis WashSaleAnalysis
	}{}

	for symbol, trades := range report.TradesBySymbol {
		analysis := AnalyzeSymbol(trades)
		for _, suspicious := range analysis.SuspiciousTrades {
			allSuspicious = append(allSuspicious, struct {
				Symbol   string
				Analysis WashSaleAnalysis
			}{
				Symbol:   symbol,
				Analysis: suspicious,
			})
		}
	}

	// Sort by absolute P/L amount (largest first)
	sort.SliceStable(allSuspicious, func(i, j int) bool {
		a, b := allSuspicious[i], allSuspicious[j]
		if math.Abs(a.Analysis.Trade.RealizedPL) == math.Abs(b.Analysis.Trade.RealizedPL) {
			if a.Symbol != b.Symbol {
				return a.Symbol < b.Symbol
			}
			if !a.Analysis.Trade.DateTime.Equal(b.Analysis.Trade.DateTime) {
				return a.Analysis.Trade.DateTime.Before(b.Analysis.Trade.DateTime)
			}
			return tradeInventoryKey(a.Analysis.Trade) < tradeInventoryKey(b.Analysis.Trade)
		}
		return math.Abs(allSuspicious[i].Analysis.Trade.RealizedPL) >
			math.Abs(allSuspicious[j].Analysis.Trade.RealizedPL)
	})

	fmt.Printf("\n=== SUSPICIOUS WASH SALE REPORT ===\n")
	fmt.Printf("Period: %s\n", report.Period)
	fmt.Printf("Account: %s\n\n", report.AccountID)

	fmt.Printf("Wash-Sale Adjustments Requiring Review: %d\n\n", len(allSuspicious))

	if len(allSuspicious) > 0 {
		fmt.Println("TOP SUSPICIOUS WASH SALES (By Loss Amount):")
		fmt.Println("Symbol | Date | Quantity | Price | P/L | Nearest Repurchase | Days Diff | Reason")
		fmt.Println("-----------------------------------------------------------------------------------")

		for _, item := range allSuspicious {
			sale := item.Analysis.Trade
			nearestInfo := "None found"
			if item.Analysis.NearestRepurchase != nil {
				nearestBuy := *item.Analysis.NearestRepurchase
				nearestInfo = fmt.Sprintf("%s (%.0f @ $%.2f)",
					nearestBuy.DateTime.Format("01/02/2006"),
					math.Abs(nearestBuy.Quantity),
					nearestBuy.TradePrice)
			}

			fmt.Printf("%s | %s | %.0f | $%.2f | $%.2f | %s | %d days | %s\n",
				item.Symbol,
				sale.DateTime.Format("01/02/2006"),
				math.Abs(sale.Quantity),
				sale.TradePrice,
				sale.RealizedPL,
				nearestInfo,
				item.Analysis.DaysDifference,
				item.Analysis.Reason)
		}

		// Group by reason
		reasonCounts := make(map[string]int)
		reasonLosses := make(map[string]float64)
		for _, item := range allSuspicious {
			reasonCounts[item.Analysis.Reason]++
			reasonLosses[item.Analysis.Reason] += math.Abs(item.Analysis.Trade.RealizedPL)
		}

		fmt.Println("\nSUMMARY BY REASON:")
		fmt.Println("Reason | Count | Total Loss Amount")
		fmt.Println("-----------------------------------")

		reasons := make([]string, 0, len(reasonCounts))
		for reason := range reasonCounts {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			count := reasonCounts[reason]
			fmt.Printf("%s | %d | $%.2f\n", reason, count, reasonLosses[reason])
		}

		fmt.Println("\nRECOMMENDATIONS:")
		fmt.Println("1. Review the largest loss amounts first, as they have the biggest tax impact")
		fmt.Println("2. Missing replacement evidence can reflect incomplete history or trades in other accounts")
		fmt.Println("3. For 'Just outside 30-day window' cases, verify the exact dates carefully")
		fmt.Println("4. Consider consulting with a tax professional for guidance on disputed wash sales")
	}

	fmt.Println("\nNOTE: This analysis is based on the activity report provided and may not include")
	fmt.Println("trades from other accounts or periods that could affect wash sale determination.")
	fmt.Println("Replacement quantities use FIFO; supply broker closed-lot details for other tax-lot elections.")
	fmt.Println("Reported P/L includes disallowed losses; a zero amount is not evidence of an invalid wash sale.")
}
