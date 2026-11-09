package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

func AnalyzeSymbolPosition(path, symbol string) error {
	return analyzeSymbolPosition(path, symbol, false)
}

func analyzeSymbolPosition(path, symbol string, printAll bool) error {
	report, err := activity.ParseActivityReport(path)
	if err != nil {
		return err
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	var trades []activity.Trade
	currencies := make(map[string]struct{})
	for _, t := range report.TradesBySymbol[symbol] {
		if t.AssetCategory == "Stocks" {
			trades = append(trades, t)
			currencies[t.Currency] = struct{}{}
		}
	}
	var snapshots []activity.MarkToMarketPerformance
	for _, row := range report.MarkToMarketPerformanceBySymbol[symbol] {
		if row.AssetCategory == "Stocks" {
			snapshots = append(snapshots, row)
		}
	}
	if len(trades) == 0 && len(snapshots) == 0 {
		return fmt.Errorf("no stock trades or position snapshot found for symbol: %s", symbol)
	}
	if len(currencies) > 1 {
		return fmt.Errorf("symbol %s has multiple trading currencies; a combined monetary position total is undefined", symbol)
	}
	fmt.Printf("\n=== %s POSITION SUMMARY ===\n\n", symbol)
	if len(trades) == 0 {
		fmt.Println("No stock transactions found in the supplied statement.")
		printStockPositionSnapshot(snapshots)
		return nil
	}

	currency := formatCurrencySet(currencies)
	sort.SliceStable(trades, func(i, j int) bool { return trades[i].DateTime.Before(trades[j].DateTime) })
	var quantity, volume, commission, pl float64
	buys, sells := 0, 0
	var lastSale, lastDateOnlySale time.Time
	var lastLongLoss, lastShortLoss, lastOtherLoss time.Time
	for _, t := range trades {
		quantity += t.Quantity
		volume += abs(t.Quantity)
		commission += t.CommFee
		pl += t.RealizedPL
		if t.IsBuy {
			buys++
		}
		if t.IsSale {
			sells++
			lastSale = t.DateTime
			if t.DateOnly {
				lastDateOnlySale = t.DateTime
			}
		}
		if t.RealizedPL >= 0 && !t.IsWashSale && !t.HasLossLot {
			continue
		}
		closes := t.HasCode("C")
		switch {
		case t.HasCode("O") && !closes:
			lastOtherLoss = t.DateTime
		case t.IsSale:
			lastLongLoss = t.DateTime
		case t.IsBuy && (closes || t.RealizedPL < 0):
			lastShortLoss = t.DateTime
		default:
			lastOtherLoss = t.DateTime
		}
	}
	fmt.Printf("Total Buy Transactions: %d\nTotal Sell Transactions: %d\n", buys, sells)
	fmt.Printf("Total Commission Paid: %s\n", formatBaseCurrency(commission, currency))
	fmt.Printf("Total Reported Realized P/L: %s\n", formatBaseCurrency(pl, currency))
	fmt.Println("Reported realized P/L includes commissions and wash-sale adjustments; it is not total economic performance.")
	fmt.Printf("Net Period Trade Qty: %g\n", quantity)
	printStockPositionSnapshot(snapshots)
	fmt.Printf("First Transaction: %s\nLast Transaction: %s\n", trades[0].DateTime.Format("January 2, 2006"), trades[len(trades)-1].DateTime.Format("January 2, 2006"))
	fmt.Printf("Total Shares Traded: %g\n", volume)
	average := 0.0
	if volume > 0 {
		average = pl / volume
	}
	fmt.Printf("Average P/L per Share: %s\n", formatBaseCurrency(average, currency))
	if printAll {
		fmt.Println("\nALL TRANSACTIONS:")
		printPositionTrades(trades, currency)
	}
	if !lastSale.IsZero() {
		printTradesAfterLastSale(trades, lastSale, lastDateOnlySale, currency)
	}
	fmt.Println("\nWASH-SALE COVERAGE:")
	asOf := report.StatementDate
	if asOf.IsZero() {
		asOf = report.PeriodEnd
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	report.PrintCoverage(asOf)
	if !lastLongLoss.IsZero() {
		start, end := washsale.Day(lastLongLoss).AddDate(0, 0, -30), washsale.Day(lastLongLoss).AddDate(0, 0, 30)
		fmt.Printf("Latest reported long loss-sale window: %s through %s\n", start.Format(time.DateOnly), end.Format(time.DateOnly))
		if !report.CoversDates(start, end) {
			fmt.Println("Statement does not cover the full window; repurchase status is unknown outside the supplied dates.")
		} else {
			fmt.Println("Statement dates cover this window. Replacement quantities and tax treatment still require lot-level review.")
		}
		fmt.Printf("First date outside that forward window: %s\n", washsale.RepurchaseDate(lastLongLoss).Format(time.DateOnly))
	}
	if !lastShortLoss.IsZero() {
		fmt.Printf("Latest reported short-cover loss or LD adjustment: %s\n", lastShortLoss.Format(time.DateOnly))
		fmt.Println("Short-cover wash-sale timing requires delivery and lot details.")
	}
	if !lastOtherLoss.IsZero() {
		fmt.Printf("Latest loss or LD adjustment requiring trade classification: %s\n", lastOtherLoss.Format(time.DateOnly))
	}
	if lastLongLoss.IsZero() && lastShortLoss.IsZero() && lastOtherLoss.IsZero() {
		fmt.Println("No net loss, LD-coded transaction or losing closed lot found in supplied trades; without closed-lot rows, individual loss lots may be hidden by order totals.")
	}
	activity.PrintWashSaleScope()
	return nil
}

func printStockPositionSnapshot(rows []activity.MarkToMarketPerformance) {
	var prior, current float64
	priorKnown, currentKnown := len(rows) > 0, len(rows) > 0
	hasOpenShares := false
	for _, row := range rows {
		prior += row.PriorQuantity
		current += row.CurrentQuantity
		priorKnown = priorKnown && !row.PriorQuantityMissing
		currentKnown = currentKnown && !row.CurrentQuantityMissing
		hasOpenShares = hasOpenShares || row.CurrentQuantity != 0
	}
	label := "IBKR"
	if len(rows) > 1 {
		label = "IBKR Net"
	}
	if priorKnown {
		fmt.Printf("%s Prior Quantity: %g\n", label, prior)
	}
	if !currentKnown {
		fmt.Println("Current position unknown: an ending quantity is unavailable in the broker snapshot.")
		return
	}
	fmt.Printf("%s Current Quantity: %g\n", label, current)
	if hasOpenShares {
		fmt.Println("Position has open shares as of the statement's ending snapshot")
	} else {
		fmt.Println("Position fully liquidated as of the statement's ending snapshot")
	}
}

func printTradesAfterLastSale(trades []activity.Trade, lastSale, lastDateOnlySale time.Time, currency string) {
	saleDay := washsale.Day(lastSale)
	saleTimeUncertain := !lastDateOnlySale.IsZero() && washsale.Day(lastDateOnlySale).Equal(saleDay)
	var later, uncertain []activity.Trade
	for _, t := range trades {
		day := washsale.Day(t.DateTime)
		switch {
		case day.After(saleDay):
			later = append(later, t)
		case day.Equal(saleDay) && t.IsBuy:
			if t.DateOnly || t.DateTime.Equal(lastSale) || (saleTimeUncertain && t.DateTime.After(lastSale)) {
				uncertain = append(uncertain, t)
			} else if t.DateTime.After(lastSale) {
				later = append(later, t)
			}
		}
	}
	if len(later) > 0 {
		fmt.Printf("\nTRANSACTIONS AFTER LAST SALE (%s):\n", saleDay.Format(time.DateOnly))
		printPositionTrades(later, currency)
	}
	if len(uncertain) > 0 {
		fmt.Printf("\nSAME-DAY BUYS WITH UNCERTAIN ORDER RELATIVE TO LAST SALE (%s):\n", saleDay.Format(time.DateOnly))
		printPositionTrades(uncertain, currency)
	}
}

func printPositionTrades(trades []activity.Trade, currency string) {
	fmt.Println("Date/Time | Type | Quantity | Price | Reported Realized P/L")
	for _, t := range trades {
		fmt.Printf("%s | %s | %g | %s | %s\n", tradeTimestamp(t), tradeSide(t), abs(t.Quantity), formatBaseCurrency(t.TradePrice, currency), formatBaseCurrency(t.RealizedPL, currency))
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
