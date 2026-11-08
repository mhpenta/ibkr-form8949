package main

import (
	"fmt"
	"strings"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
	"github.com/mhpenta/ibkr-form8949/internal/xlsx"
)

const (
	spreadsheetDisclaimer = "Not tax advice. Check figures against your broker's official documents."
	washSaleHint          = "Run --wash-sale-periods and --safe-repurchase for loss-sale windows."
)

// writeTradeRowWorkbook writes the stock trade-row analysis: every security on one
// sheet and the report totals on another. performances must be sorted best first.
func writeTradeRowWorkbook(path, title, period string, performances []TradeRowSecurityPerformance, currencies map[string]struct{}) error {
	var book xlsx.Workbook

	securities := book.AddSheet("Securities")
	securities.Header("Symbol", "Currency", "Realized P/L", "Commission", "Trades", "Buys", "Sells", "Share Volume", "Net Period Qty")
	securities.Widths(14, 10, 16, 14, 9, 9, 9, 14, 15)
	for _, perf := range performances {
		securities.Row(
			xlsx.Text(perf.Symbol),
			xlsx.Text(formatCurrencySet(perf.Currencies)),
			xlsx.Money(perf.TradeRowRealizedPL),
			xlsx.Money(perf.TradeRowCommission),
			xlsx.Int(perf.TotalTrades),
			xlsx.Int(perf.BuyTrades),
			xlsx.Int(perf.SellTrades),
			xlsx.Number(perf.ShareVolume),
			xlsx.Number(perf.NetPeriodQuantity),
		)
	}

	totals := summarizeTradeRows(performances)
	currencyLabel := formatCurrencySet(currencies)
	if len(currencies) == 0 {
		currencyLabel = ""
	}

	summary := book.AddSheet("Summary")
	summary.Widths(34, 18, 12)
	summary.Row(xlsx.Title(title))
	summary.Row(xlsx.Text("Statement period"), xlsx.Text(period))
	summary.Row(xlsx.Text("Currencies"), xlsx.Text(currencyLabel))
	summary.Blank()
	summary.Row(xlsx.Bold("Totals"))
	summary.Row(xlsx.Text("Securities traded"), xlsx.Int(totals.Securities))
	summary.Row(xlsx.Text("Trade-row realized P/L"), xlsx.Money(totals.RealizedPL))
	summary.Row(xlsx.Text("Trades"), xlsx.Int(totals.Trades))
	summary.Row(xlsx.Text("Trade-row commission"), xlsx.Money(totals.Commission))
	summary.Row(xlsx.Text("Profitable securities"), xlsx.Int(totals.Winners), xlsx.Percent(fraction(totals.Winners, totals.Securities)))
	summary.Row(xlsx.Text("Losing securities"), xlsx.Int(totals.Losers), xlsx.Percent(fraction(totals.Losers, totals.Securities)))
	summary.Blank()
	summary.Row(xlsx.Bold("Notes"))
	summary.Row(xlsx.Text("Scope: stock Trades rows only; excludes options, dividends, interest, transfers, NAV, and base-currency performance summaries."))
	summary.Row(xlsx.Text("Amounts are raw IBKR trade-row values in each instrument's trading currency."))
	if len(currencies) > 1 {
		summary.Row(xlsx.Text("Several currencies are present: totals add mixed currencies and are not account-level USD gains."))
	}
	summary.Row(xlsx.Text("Reported realized P/L already includes commissions and wash-sale adjustments; commissions are shown separately for reference."))
	summary.Row(xlsx.Text("Net Period Qty is buys minus sells within the statement period, not the ending position when the period starts with open holdings."))
	summary.Row(xlsx.Text(washSaleHint))
	summary.Row(xlsx.Text(spreadsheetDisclaimer))

	return book.Save(path)
}

// writeMarkToMarketWorkbook writes the base-currency mark-to-market analysis: every
// security, a per-asset-category breakdown, and account totals.
func writeMarkToMarketWorkbook(path, title string, report *activity.ActivityReport) error {
	baseCurrency := normalizeCurrency(report.BaseCurrency)
	performances := sortedMarkToMarketPerformance(report.MarkToMarketPerformance)
	money := func(label string) string { return fmt.Sprintf("%s (%s)", label, baseCurrency) }

	var book xlsx.Workbook

	securities := book.AddSheet("Securities")
	securities.Header("Symbol", "Asset Category", "Prior Qty", "Current Qty",
		money("Position P/L"), money("Transaction P/L"), money("Commissions"), money("Other"), money("Total P/L"))
	securities.Widths(24, 26, 11, 12, 18, 21, 18, 14, 16)
	for _, perf := range performances {
		securities.Row(
			xlsx.Text(perf.Symbol),
			xlsx.Text(perf.AssetCategory),
			xlsx.Number(perf.PriorQuantity),
			xlsx.Number(perf.CurrentQuantity),
			xlsx.Money(perf.PositionPL),
			xlsx.Money(perf.TransactionPL),
			xlsx.Money(perf.CommissionPL),
			xlsx.Money(perf.OtherPL),
			xlsx.Money(perf.TotalPL),
		)
	}

	assets := summarizeMarkToMarketByAsset(performances)
	byAsset := book.AddSheet("By Asset Category")
	byAsset.Header("Asset Category", "Symbols",
		money("Position P/L"), money("Transaction P/L"), money("Commissions"), money("Other"), money("Total P/L"))
	byAsset.Widths(26, 10, 18, 21, 18, 14, 16)
	var total MarkToMarketAssetSummary
	for _, asset := range assets {
		byAsset.Row(
			xlsx.Text(asset.AssetCategory),
			xlsx.Int(asset.SymbolCount),
			xlsx.Money(asset.PositionPL),
			xlsx.Money(asset.TransactionPL),
			xlsx.Money(asset.CommissionPL),
			xlsx.Money(asset.OtherPL),
			xlsx.Money(asset.TotalPL),
		)
		total.SymbolCount += asset.SymbolCount
		total.PositionPL += asset.PositionPL
		total.TransactionPL += asset.TransactionPL
		total.CommissionPL += asset.CommissionPL
		total.OtherPL += asset.OtherPL
		total.TotalPL += asset.TotalPL
	}

	var winners, losers int
	for _, perf := range performances {
		if perf.TotalPL > 0 {
			winners++
		} else if perf.TotalPL < 0 {
			losers++
		}
	}

	summary := book.AddSheet("Summary")
	summary.Widths(34, 18, 12)
	summary.Row(xlsx.Title(title))
	summary.Row(xlsx.Text("Statement period"), xlsx.Text(report.Period))
	summary.Row(xlsx.Text("Base currency"), xlsx.Text(baseCurrency))

	if nav := report.ChangeInNAV; nav.HasData() {
		summary.Blank()
		summary.Row(xlsx.Bold("Account NAV"))
		for _, line := range []struct {
			label string
			value float64
		}{
			{"Starting NAV", nav.StartingValue},
			{"Ending NAV", nav.EndingValue},
			{"Net NAV change", nav.NetNAVChange()},
			{"Deposits & withdrawals", nav.DepositsWithdrawals},
			{"Position transfers", nav.PositionTransfers},
			{"External flows", nav.ExternalFlows()},
			{"Gain before external flows", nav.GainBeforeExternalFlows()},
			{"Mark-to-market", nav.MarkToMarket},
			{"Dividends", nav.Dividends},
			{"Interest", nav.Interest},
			{"Commissions", nav.Commissions},
		} {
			summary.Row(xlsx.Text(line.label), xlsx.Money(line.value))
		}
	}

	summary.Blank()
	summary.Row(xlsx.Bold("Mark-to-market totals"))
	summary.Row(xlsx.Text("Detail rows"), xlsx.Int(len(performances)))
	summary.Row(xlsx.Text("Total MTM P/L"), xlsx.Money(total.TotalPL))
	summary.Row(xlsx.Text("Position P/L"), xlsx.Money(total.PositionPL))
	summary.Row(xlsx.Text("Transaction P/L"), xlsx.Money(total.TransactionPL))
	summary.Row(xlsx.Text("Commissions"), xlsx.Money(total.CommissionPL))
	summary.Row(xlsx.Text("Other"), xlsx.Money(total.OtherPL))
	summary.Row(xlsx.Text("Profitable symbols"), xlsx.Int(winners), xlsx.Percent(fraction(winners, len(performances))))
	summary.Row(xlsx.Text("Losing symbols"), xlsx.Int(losers), xlsx.Percent(fraction(losers, len(performances))))
	if ibkrTotal, ok := findMarkToMarketTotal(report.MarkToMarketPerformance, "Total (All Assets)"); ok {
		summary.Row(xlsx.Text("IBKR Total (All Assets)"), xlsx.Money(ibkrTotal.TotalPL))
	}

	summary.Blank()
	summary.Row(xlsx.Bold("Notes"))
	summary.Row(xlsx.Text(fmt.Sprintf("Scope: IBKR Mark-to-Market Performance Summary detail rows; amounts are account base currency (%s).", baseCurrency)))
	summary.Row(xlsx.Text("Includes stocks, equity/index options, and forex detail rows; excludes broker interest and other fee rows outside Total (All Assets)."))
	summary.Row(xlsx.Text("This is account-level mark-to-market performance, not tax-realized Form 8949 P/L."))
	summary.Row(xlsx.Text(washSaleHint))
	summary.Row(xlsx.Text(spreadsheetDisclaimer))

	return book.Save(path)
}

func fraction(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) / float64(total)
}

// validSpreadsheetPath rejects output paths that would not open as a workbook.
func validSpreadsheetPath(path string) error {
	if !strings.HasSuffix(strings.ToLower(path), ".xlsx") {
		return fmt.Errorf("--xlsx path must end in .xlsx: %q", path)
	}
	return nil
}
