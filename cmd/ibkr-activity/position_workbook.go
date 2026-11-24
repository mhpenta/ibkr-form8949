package main

import (
	"fmt"
	"math"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
	"github.com/mhpenta/ibkr-form8949/internal/xlsx"
)

// writePositionWorkbook writes one stock's position report: a summary, every
// transaction with the running position, monthly activity, price ranges, and
// loss sales with their wash-sale windows.
func writePositionWorkbook(path string, v positionView) error {
	var book xlsx.Workbook
	writePositionSummarySheet(book.AddSheet("Summary"), v)
	writePositionTransactionsSheet(book.AddSheet("Transactions"), v)
	writePositionMonthlySheet(book.AddSheet("Monthly"), v)
	writePositionPriceSheet(book.AddSheet("Price Ranges"), v)
	writePositionLossSalesSheet(book.AddSheet("Loss Sales"), v)
	return book.Save(path)
}

func writePositionSummarySheet(sheet *xlsx.Sheet, v positionView) {
	s := v.stats
	sheet.Widths(44, 18, 18, 18, 18)
	sheet.Row(xlsx.Title(v.symbol + " position"))
	sheet.Row(xlsx.Text("Statement period"), xlsx.Text(v.report.Period))
	sheet.Row(xlsx.Text("Trading currency"), xlsx.Text(v.currency))
	sheet.Blank()
	if sources := v.report.Sources; len(sources) > 1 {
		sheet.Row(xlsx.Bold("Statements combined"), xlsx.Bold("Period"), xlsx.Bold("Trades used"), xlsx.Bold("Trades in file"))
		for _, source := range sources {
			sheet.Row(xlsx.Text(source.Name), xlsx.Text(source.Period), xlsx.Int(source.TradesUsed), xlsx.Int(source.Trades))
		}
		for _, gap := range v.report.Gaps() {
			sheet.Row(xlsx.Text("Not covered by any statement"), xlsx.Text(gap.String()))
		}
		sheet.Blank()
	}

	sheet.Row(xlsx.Bold("Position"))
	sheet.Row(xlsx.Text("IBKR quantity at statement start"), quantityCell(v.snapshot.prior, v.snapshot.priorKnown))
	sheet.Row(xlsx.Text("Net shares traded (bought − sold)"), xlsx.Number(v.netTraded()))
	sheet.Row(xlsx.Text("IBKR quantity at statement end"), quantityCell(v.snapshot.current, v.snapshot.currentKnown))
	reconciles := "Unknown (IBKR quantities unavailable)"
	if matches, ok := v.snapshot.reconciles(v.netTraded()); ok && matches {
		reconciles = "Yes"
	} else if ok {
		reconciles = "No: splits, transfers or other corporate actions may be missing"
	}
	sheet.Row(xlsx.Text("Start + trades = end"), xlsx.Text(reconciles))
	final := s.Final(v.snapshot.opening())
	switch {
	case math.Abs(final.Position) <= 1e-9:
		sheet.Row(xlsx.Text("Average price of open shares"), xlsx.Text("No open shares after the last trade"))
	case final.OpenPriceKnown:
		sheet.Row(xlsx.Text("Average price of open shares"), xlsx.Price(final.OpenPrice))
	default:
		sheet.Row(xlsx.Text("Average price of open shares"), xlsx.Text("Unknown: shares from before the first trade are still open"))
	}
	if len(v.trades) > 0 {
		sheet.Row(xlsx.Text("First trade"), tradeDateCell(s.Steps[0].Trade))
		sheet.Row(xlsx.Text("Last trade"), tradeDateCell(s.Steps[len(s.Steps)-1].Trade))
	}
	sheet.Blank()

	sides := []activity.SideStats{s.Buys, s.Sells}
	labels := []string{"Buys", "Sells"}
	if s.HasShortActivity() {
		sides, labels = nil, nil
		for _, action := range activity.TradeActions {
			sides = append(sides, s.Actions[action])
			labels = append(labels, action.String())
		}
	}
	writeSideTable(sheet, labels, sides)
	sheet.Blank()

	sheet.Row(xlsx.Bold("Realized"))
	sheet.Row(xlsx.Text("Reported realized P/L"), xlsx.Money(s.Buys.RealizedPL+s.Sells.RealizedPL))
	sheet.Row(xlsx.Text("Commissions and fees paid"), xlsx.Money(s.Buys.Commission+s.Sells.Commission))
	if s.SoldShares > 0 {
		sell := s.Actions[activity.ActionSell]
		sheet.Row(xlsx.Text("Long shares sold"), xlsx.Number(s.SoldShares))
		sheet.Row(xlsx.Text("Average IBKR cost basis of shares sold"), xlsx.Price(s.AverageSoldCost()))
		sheet.Row(xlsx.Text("Average sale price after commissions"), xlsx.Price(sell.AverageNetPrice()))
		sheet.Row(xlsx.Text("Reported realized P/L per share sold"), xlsx.Price(sell.RealizedPL/sell.Shares))
	}
	lossSales, washSales := 0, 0
	for _, t := range v.trades {
		if t.IsLossSale() {
			lossSales++
		}
		if t.IsSale && t.IsWashSale {
			washSales++
		}
	}
	sheet.Row(xlsx.Text("Loss sales (see Loss Sales sheet)"), xlsx.Int(lossSales))
	sheet.Row(xlsx.Text("Sales IBKR coded as wash sales (LD)"), xlsx.Int(washSales))
	sheet.Blank()

	sheet.Row(xlsx.Bold("Notes"))
	notes := []string{
		"Scope: stock Trades rows for this symbol only; dividends, options and corporate actions are not included.",
		"Average prices are weighted by shares. 'With commissions' adds commissions to purchases and subtracts them from sales.",
		"Median price is the price at which half the shares traded below and half above.",
		"Average IBKR cost basis of shares sold comes from the broker's Basis column, so it includes shares bought before this statement.",
		"Average price of open shares uses the average-cost method on trade prices. It is not IBKR's tax-lot basis, which follows each sale's lot election and wash-sale adjustments.",
		"Loss Sales lists purchases within 30 days either side of each loss sale, including shares the sale itself disposed of; replacement matching is shown only for sales IBKR coded LD.",
		"Reported realized P/L already includes commissions and wash-sale adjustments.",
	}
	if !v.snapshot.priorKnown {
		notes = append(notes, "IBKR's opening quantity is unavailable: running positions assume no shares were held before the first trade.")
	}
	if s.HasShortActivity() {
		notes = append(notes, "Buys and sells are split by IBKR's O (open) and C (close) codes; trades without codes count as long buys and sales.")
	}
	if len(v.report.Sources) > 1 {
		notes = append(notes, "Combined statements: where periods overlap, each day's trades come from the most recently generated statement covering it. Days no statement covers are missing.")
	}
	notes = append(notes, washSaleHint, spreadsheetDisclaimer)
	for _, note := range notes {
		sheet.Row(xlsx.Text(note))
	}
}

func writeSideTable(sheet *xlsx.Sheet, labels []string, sides []activity.SideStats) {
	header := []xlsx.Cell{xlsx.Bold("Trades")}
	for _, label := range labels {
		header = append(header, xlsx.Bold(label))
	}
	sheet.Row(header...)
	rows := []struct {
		label string
		cell  func(activity.SideStats) xlsx.Cell
	}{
		{"Number of trades", func(s activity.SideStats) xlsx.Cell { return xlsx.Int(s.Trades) }},
		{"Shares", func(s activity.SideStats) xlsx.Cell { return xlsx.Number(s.Shares) }},
		{"Average price", func(s activity.SideStats) xlsx.Cell { return xlsx.Price(s.AveragePrice()) }},
		{"Average price with commissions", func(s activity.SideStats) xlsx.Cell { return xlsx.Price(s.AverageNetPrice()) }},
		{"Lowest price", func(s activity.SideStats) xlsx.Cell { return xlsx.Price(s.LowPrice) }},
		{"Median price (by shares)", func(s activity.SideStats) xlsx.Cell { return xlsx.Price(s.MedianPrice) }},
		{"Highest price", func(s activity.SideStats) xlsx.Cell { return xlsx.Price(s.HighPrice) }},
		{"Smallest trade (shares)", func(s activity.SideStats) xlsx.Cell { return xlsx.Number(s.SmallestTrade) }},
		{"Median trade (shares)", func(s activity.SideStats) xlsx.Cell { return xlsx.Number(s.MedianTrade) }},
		{"Largest trade (shares)", func(s activity.SideStats) xlsx.Cell { return xlsx.Number(s.LargestTrade) }},
		{"Value at trade prices", func(s activity.SideStats) xlsx.Cell { return xlsx.Money(s.Value) }},
		{"Commissions and fees", func(s activity.SideStats) xlsx.Cell { return xlsx.Money(s.Commission) }},
		{"Reported realized P/L", func(s activity.SideStats) xlsx.Cell { return xlsx.Money(s.RealizedPL) }},
		{"First trade", func(s activity.SideStats) xlsx.Cell { return dateCell(s.First) }},
		{"Last trade", func(s activity.SideStats) xlsx.Cell { return dateCell(s.Last) }},
	}
	for _, row := range rows {
		cells := []xlsx.Cell{xlsx.Text(row.label)}
		for _, side := range sides {
			if side.Trades == 0 {
				cells = append(cells, xlsx.Empty())
				continue
			}
			cells = append(cells, row.cell(side))
		}
		sheet.Row(cells...)
	}
}

func writePositionTransactionsSheet(sheet *xlsx.Sheet, v positionView) {
	combined := len(v.report.Sources) > 1
	header := []string{"Date/Time", "Action", "Shares", "Price", "Value", "Proceeds", "Commission", "Net Cash", "IBKR Basis",
		"Realized P/L", "Codes", "Wash Sale (LD)", "Position After", "Avg Open Price"}
	if combined {
		header = append(header, "Statement")
	}
	sheet.Header(header...)
	sheet.Widths(20, 14, 10, 12, 14, 14, 12, 14, 14, 14, 10, 14, 14, 15, 28)
	for _, step := range v.stats.Steps {
		t := step.Trade
		open := xlsx.Empty()
		if step.OpenPriceKnown && step.Position != 0 {
			open = xlsx.Price(step.OpenPrice)
		}
		realized := xlsx.Money(t.RealizedPL)
		if t.RealizedPLMissing {
			realized = xlsx.Empty()
		}
		cells := []xlsx.Cell{
			tradeDateCell(t),
			xlsx.Text(step.Action.String()),
			xlsx.Number(math.Abs(t.Quantity)),
			xlsx.Price(t.TradePrice),
			xlsx.Money(math.Abs(t.Quantity) * t.TradePrice),
			xlsx.Money(t.Proceeds),
			xlsx.Money(t.CommFee),
			xlsx.Money(t.Proceeds + t.CommFee),
			xlsx.Money(t.Basis),
			realized,
			xlsx.Text(t.Code),
			xlsx.Text(yesNo(t.IsWashSale)),
			xlsx.Number(step.Position),
			open,
		}
		if combined {
			cells = append(cells, xlsx.Text(t.Source))
		}
		sheet.Row(cells...)
	}
}

func writePositionMonthlySheet(sheet *xlsx.Sheet, v positionView) {
	sheet.Header("Month", "Buy Trades", "Shares Bought", "Avg Buy Price", "Sell Trades", "Shares Sold", "Avg Sell Price",
		"Net Shares", "Position at Month End", "Realized P/L", "Commission")
	sheet.Widths(10, 11, 14, 14, 11, 13, 14, 12, 20, 14, 12)
	for _, m := range v.stats.Months {
		sheet.Row(
			xlsx.Text(m.Month.Format("2006-01")),
			xlsx.Int(m.Buys.Trades),
			xlsx.Number(m.Buys.Shares),
			averageCell(m.Buys),
			xlsx.Int(m.Sells.Trades),
			xlsx.Number(m.Sells.Shares),
			averageCell(m.Sells),
			xlsx.Number(m.Buys.Shares-m.Sells.Shares),
			xlsx.Number(m.EndPosition),
			xlsx.Money(m.Buys.RealizedPL+m.Sells.RealizedPL),
			xlsx.Money(m.Buys.Commission+m.Sells.Commission),
		)
	}
}

func writePositionPriceSheet(sheet *xlsx.Sheet, v positionView) {
	sheet.Header("Price From", "Price To", "Shares Bought", "% of Bought", "Shares Sold", "% of Sold", "Buy Trades", "Sell Trades")
	sheet.Widths(12, 12, 14, 12, 13, 11, 11, 11)
	bought, sold := v.stats.Buys.Shares, v.stats.Sells.Shares
	for _, band := range v.stats.PriceBands {
		sheet.Row(
			xlsx.Price(band.Low),
			xlsx.Price(band.High),
			xlsx.Number(band.Bought),
			xlsx.Percent(ratioOrZero(band.Bought, bought)),
			xlsx.Number(band.Sold),
			xlsx.Percent(ratioOrZero(band.Sold, sold)),
			xlsx.Int(band.BuyTrades),
			xlsx.Int(band.SellTrades),
		)
	}
}

// writePositionLossSalesSheet lists each loss sale with its ±30-day window, whether
// the statement covers that window, and the purchases inside it.
func writePositionLossSalesSheet(sheet *xlsx.Sheet, v positionView) {
	sheet.Header("Sale Date", "Shares", "Price", "Realized P/L", "Wash Sale (LD)", "Window Start", "Window End",
		"First Date Outside", "Statement Covers Window", "Shares Bought in Window", "Replacement Shares Matched", "Review Note")
	sheet.Widths(20, 10, 12, 14, 14, 13, 13, 17, 22, 22, 24, 50)
	checks := make(map[string]activity.WashSaleAnalysis)
	for _, check := range v.analysis.WashSaleChecks {
		checks[tradeKey(check.Trade)] = check
	}
	for _, step := range v.stats.Steps {
		sale := step.Trade
		if !sale.IsLossSale() {
			continue
		}
		day := washsale.Day(sale.DateTime)
		start, end := day.AddDate(0, 0, -30), day.AddDate(0, 0, 30)
		var bought float64
		for _, other := range v.stats.Steps {
			if other.Action != activity.ActionBuy {
				continue
			}
			if d := washsale.Day(other.Trade.DateTime); !d.Before(start) && !d.After(end) {
				bought += other.Trade.Quantity
			}
		}
		matched, note := xlsx.Empty(), xlsx.Empty()
		if check, ok := checks[tradeKey(sale)]; ok {
			matched = xlsx.Number(check.MatchedQuantity)
			note = xlsx.Text(check.Reason)
		}
		sheet.Row(
			tradeDateCell(sale),
			xlsx.Number(math.Abs(sale.Quantity)),
			xlsx.Price(sale.TradePrice),
			xlsx.Money(sale.RealizedPL),
			xlsx.Text(yesNo(sale.IsWashSale)),
			xlsx.Date(start),
			xlsx.Date(end),
			xlsx.Date(washsale.RepurchaseDate(sale.DateTime)),
			xlsx.Text(yesNo(v.report.CoversDates(start, end))),
			xlsx.Number(bought),
			matched,
			note,
		)
	}
}

// tradeKey identifies a trade row well enough to join the wash-sale checks.
func tradeKey(t activity.Trade) string {
	return fmt.Sprint(t.Account, t.DateTime.UnixNano(), t.Quantity, t.TradePrice, t.Proceeds, t.Code)
}

func tradeDateCell(t activity.Trade) xlsx.Cell {
	if t.DateOnly {
		return xlsx.Date(t.DateTime)
	}
	return xlsx.DateTime(t.DateTime)
}

func dateCell(t time.Time) xlsx.Cell {
	if t.IsZero() {
		return xlsx.Empty()
	}
	return xlsx.Date(t)
}

func quantityCell(quantity float64, known bool) xlsx.Cell {
	if !known {
		return xlsx.Text("Unknown")
	}
	return xlsx.Number(quantity)
}

func averageCell(side activity.SideStats) xlsx.Cell {
	if side.Trades == 0 {
		return xlsx.Empty()
	}
	return xlsx.Price(side.AveragePrice())
}

func ratioOrZero(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
