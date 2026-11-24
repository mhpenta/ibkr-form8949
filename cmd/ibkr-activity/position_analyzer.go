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

func AnalyzeSymbolPosition(path, symbol string) error {
	return analyzeSymbolPosition([]string{path}, symbol, false, "")
}

// analyzeSymbolPosition prints one stock's trades, prices and holdings. With
// printAll it lists every trade; with xlsxPath it also writes a workbook.
func analyzeSymbolPosition(files []string, symbol string, printAll bool, xlsxPath string) error {
	report, err := loadStatements(files)
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
	currency := formatCurrencySet(currencies)
	if len(trades) == 0 {
		currency = normalizeCurrency(report.BaseCurrency)
	}
	snapshot := snapshotTotals(snapshots)
	view := positionView{
		symbol: symbol, currency: currency, report: report, trades: trades, snapshot: snapshot,
		stats:    activity.AnalyzePosition(trades, snapshot.opening()),
		analysis: report.AnalyzeTrades(symbol),
	}
	fmt.Printf("\n=== %s POSITION SUMMARY ===\n\n", symbol)
	if len(trades) == 0 {
		fmt.Println("No stock transactions found in the supplied statement.")
		printStockPositionSnapshot(snapshots)
		return writePositionWorkbookIfRequested(xlsxPath, view)
	}

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
	printPriceSummary(view)
	if printAll {
		fmt.Println("\nALL TRANSACTIONS:")
		printPositionTimeline(view.stats.Steps, currency)
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
	return writePositionWorkbookIfRequested(xlsxPath, view)
}

// positionSnapshot totals a symbol's Mark-to-Market quantities across rows.
type positionSnapshot struct {
	prior, current           float64
	priorKnown, currentKnown bool
}

func snapshotTotals(rows []activity.MarkToMarketPerformance) positionSnapshot {
	s := positionSnapshot{priorKnown: len(rows) > 0, currentKnown: len(rows) > 0}
	for _, row := range rows {
		s.prior += row.PriorQuantity
		s.current += row.CurrentQuantity
		s.priorKnown = s.priorKnown && !row.PriorQuantityMissing
		s.currentKnown = s.currentKnown && !row.CurrentQuantityMissing
	}
	return s
}

// opening is the quantity held before the first trade, or 0 when unknown.
func (s positionSnapshot) opening() float64 {
	if s.priorKnown {
		return s.prior
	}
	return 0
}

// reconciles reports whether prior quantity plus trades equals the ending quantity.
// ok is false when either broker quantity is unavailable.
func (s positionSnapshot) reconciles(netTraded float64) (matches, ok bool) {
	if !s.priorKnown || !s.currentKnown {
		return false, false
	}
	return math.Abs(s.prior+netTraded-s.current) <= 1e-6, true
}

// positionView is everything the position report and workbook show.
type positionView struct {
	symbol, currency string
	report           *activity.ActivityReport
	trades           []activity.Trade
	snapshot         positionSnapshot
	stats            activity.PositionStats
	analysis         activity.SymbolAnalysis
}

func (v positionView) netTraded() float64 {
	return v.stats.Buys.Shares - v.stats.Sells.Shares
}

func writePositionWorkbookIfRequested(path string, view positionView) error {
	if path == "" {
		return nil
	}
	if err := writePositionWorkbook(path, view); err != nil {
		return fmt.Errorf("write spreadsheet: %w", err)
	}
	fmt.Printf("\nSpreadsheet written to %s\n", path)
	return nil
}

func printPriceSummary(v positionView) {
	s, c := v.stats, v.currency
	fmt.Printf("\nBUYS AND SELLS (%s, averages weighted by shares):\n", c)
	printSide("Buy", s.Buys, c)
	printSide("Sell", s.Sells, c)
	if s.SoldShares > 0 {
		fmt.Printf("Average IBKR Cost of Shares Sold: %s over %g shares (broker basis, including shares bought before this statement)\n",
			formatPrice(s.AverageSoldCost(), c), s.SoldShares)
	}
	if s.HasShortActivity() {
		fmt.Println("Short activity (from IBKR O/C codes):")
		for _, action := range activity.TradeActions {
			if side := s.Actions[action]; side.Trades > 0 {
				fmt.Printf("  %s: %s average over %g shares in %d %s\n", action, formatPrice(side.AveragePrice(), c), side.Shares, side.Trades, plural(side.Trades, "trade"))
			}
		}
	}
	if !v.snapshot.priorKnown {
		fmt.Println("Opening quantity unavailable: open-share figures assume no shares were held before the first trade.")
	}
	if matches, ok := v.snapshot.reconciles(v.netTraded()); ok && !matches {
		fmt.Printf("Trades do not reconcile with IBKR quantities (prior %g + traded %g, ending %g): splits, transfers or other corporate actions may be missing, so open-share figures can be wrong.\n",
			v.snapshot.prior, v.netTraded(), v.snapshot.current)
	}
	final := s.Final(v.snapshot.opening())
	switch {
	case math.Abs(final.Position) <= 1e-9:
	case final.OpenPriceKnown:
		side := "long"
		if final.Position < 0 {
			side = "short"
		}
		fmt.Printf("Average Price of Open Shares: %s on %g %s shares (average-cost method, before commissions; not IBKR tax basis)\n",
			formatPrice(final.OpenPrice, c), math.Abs(final.Position), side)
	default:
		fmt.Println("Average Price of Open Shares: unknown, because shares held before the first trade are still open.")
	}
}

func printSide(label string, side activity.SideStats, currency string) {
	if side.Trades == 0 {
		fmt.Printf("%ss: none\n", label)
		return
	}
	fmt.Printf("Average %s Price: %s (%s with commissions) over %g shares in %d %s\n", label,
		formatPrice(side.AveragePrice(), currency), formatPrice(side.AverageNetPrice(), currency), side.Shares, side.Trades, plural(side.Trades, "trade"))
	fmt.Printf("%s Price Range: %s low, %s median, %s high\n", label,
		formatPrice(side.LowPrice, currency), formatPrice(side.MedianPrice, currency), formatPrice(side.HighPrice, currency))
	fmt.Printf("%s Trade Size: %g smallest, %g median, %g largest\n", label, side.SmallestTrade, side.MedianTrade, side.LargestTrade)
}

func printPositionTimeline(steps []activity.PositionStep, currency string) {
	fmt.Println("Date/Time | Action | Quantity | Price | Commission | Reported Realized P/L | Codes | Position After | Avg Open Price")
	for _, step := range steps {
		t := step.Trade
		open := "--"
		if step.OpenPriceKnown && step.Position != 0 {
			open = formatPrice(step.OpenPrice, currency)
		} else if step.OpenPriceKnown {
			open = "flat"
		}
		fmt.Printf("%s | %s | %g | %s | %s | %s | %s | %g | %s\n", tradeTimestamp(t), step.Action, abs(t.Quantity),
			formatPrice(t.TradePrice, currency), formatBaseCurrency(t.CommFee, currency), formatBaseCurrency(t.RealizedPL, currency),
			t.Code, step.Position, open)
	}
}

// formatPrice shows a per-share price with two to four decimals.
func formatPrice(price float64, currency string) string {
	s := strconv.FormatFloat(price, 'f', 4, 64)
	for strings.HasSuffix(s, "0") && len(s)-strings.IndexByte(s, '.') > 3 {
		s = s[:len(s)-1]
	}
	if currency == "" || currency == "USD" {
		if strings.HasPrefix(s, "-") {
			return "-$" + s[1:]
		}
		return "$" + s
	}
	return s + " " + currency
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
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
