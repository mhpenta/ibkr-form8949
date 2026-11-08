package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
)

var reportYearPattern = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)

// TradeRowSecurityPerformance contains raw trade-row metrics for a security.
type TradeRowSecurityPerformance struct {
	Symbol             string
	TradeRowRealizedPL float64
	TotalTrades        int
	BuyTrades          int
	SellTrades         int
	ShareVolume        float64 // Absolute shares traded during the statement period
	TradeRowCommission float64
	NetPeriodQuantity  float64 // Buys minus sells during the statement period
	Currencies         map[string]struct{}
}

// MarkToMarketSecurityPerformance contains base-currency MTM metrics for a symbol.
type MarkToMarketSecurityPerformance struct {
	AssetCategory   string
	Symbol          string
	PriorQuantity   float64
	CurrentQuantity float64
	PositionPL      float64
	TransactionPL   float64
	CommissionPL    float64
	OtherPL         float64
	TotalPL         float64
}

type MarkToMarketAssetSummary struct {
	AssetCategory string
	SymbolCount   int
	PositionPL    float64
	TransactionPL float64
	CommissionPL  float64
	OtherPL       float64
	TotalPL       float64
}

// tradeRowTotals summarizes trade-row performance across securities.
type tradeRowTotals struct {
	Securities int
	RealizedPL float64
	Trades     int
	Commission float64
	Winners    int
	Losers     int
}

func summarizeTradeRows(performances []TradeRowSecurityPerformance) tradeRowTotals {
	totals := tradeRowTotals{Securities: len(performances)}
	for _, perf := range performances {
		totals.RealizedPL += perf.TradeRowRealizedPL
		totals.Trades += perf.TotalTrades
		totals.Commission += perf.TradeRowCommission
		if perf.TradeRowRealizedPL > 0 {
			totals.Winners++
		} else if perf.TradeRowRealizedPL < 0 {
			totals.Losers++
		}
	}
	return totals
}

// AnalyzeAllSecurities analyzes all securities in the IBKR activity report. When
// xlsxPath is set, it also writes every security, not just the top and bottom 20,
// to a spreadsheet at that path.
func AnalyzeAllSecurities(csvFilePath, xlsxPath string) error {
	report, err := activity.ParseActivityReport(csvFilePath)
	if err != nil {
		return err
	}
	records := [][]string{{"Statement", "Data", "Period", report.Period}}
	if len(report.MarkToMarketPerformance) > 0 {
		printMarkToMarketPerformanceReport(records, report)
		if xlsxPath != "" {
			if err := writeMarkToMarketWorkbook(xlsxPath, markToMarketReportTitle(records), report); err != nil {
				return fmt.Errorf("write spreadsheet: %w", err)
			}
			fmt.Printf("\nSpreadsheet written to %s\n", xlsxPath)
		}
		return nil
	}

	// Process trades by symbol
	securityMap := make(map[string]*TradeRowSecurityPerformance)
	reportCurrencies := make(map[string]struct{})

	for _, trade := range report.Trades {
		if trade.AssetCategory != "Stocks" {
			continue
		}
		symbol := trade.Symbol
		key := symbol + "\x00" + trade.Currency

		// Get or create performance record
		perf, exists := securityMap[key]
		if !exists {
			perf = &TradeRowSecurityPerformance{
				Symbol:     symbol,
				Currencies: make(map[string]struct{}),
			}
			securityMap[key] = perf
		}

		currency := normalizeCurrency(trade.Currency)
		perf.Currencies[currency] = struct{}{}
		reportCurrencies[currency] = struct{}{}

		quantity, realizedPL, commission := trade.Quantity, trade.RealizedPL, trade.CommFee

		// Update metrics
		perf.TradeRowRealizedPL += realizedPL
		perf.TotalTrades++
		perf.TradeRowCommission += commission
		perf.NetPeriodQuantity += quantity
		perf.ShareVolume += abs(quantity)

		if quantity > 0 {
			perf.BuyTrades++
		} else if quantity < 0 {
			perf.SellTrades++
		}
	}

	// Convert to slice for sorting
	var performances []TradeRowSecurityPerformance
	for _, perf := range securityMap {
		// Only include securities with actual trades (not just dividends or other events)
		if perf.TotalTrades > 0 {
			performances = append(performances, *perf)
		}
	}

	// Sort by P/L (best performers first)
	sort.Slice(performances, func(i, j int) bool {
		if performances[i].TradeRowRealizedPL == performances[j].TradeRowRealizedPL {
			return performances[i].Symbol+formatCurrencySet(performances[i].Currencies) < performances[j].Symbol+formatCurrencySet(performances[j].Currencies)
		}
		return performances[i].TradeRowRealizedPL > performances[j].TradeRowRealizedPL
	})

	if xlsxPath != "" {
		if err := writeTradeRowWorkbook(xlsxPath, securityReportTitle(records), report.Period, performances, reportCurrencies); err != nil {
			return fmt.Errorf("write spreadsheet: %w", err)
		}
	}

	// Print results
	fmt.Printf("\n=== %s ===\n\n", securityReportTitle(records))
	printTradeRowScope(reportCurrencies)

	fmt.Printf("BEST PERFORMERS (TOP 20):\n")
	fmt.Printf("Symbol | Trade-Row Realized P/L | Currency | # Trades | Volume | Net Period Qty\n")
	fmt.Printf("----------------------------------------------------------------------------\n")

	for i, perf := range performances {
		if i < 20 {
			fmt.Printf("%s | %.2f | %s | %d | %.0f | %.0f\n",
				perf.Symbol,
				perf.TradeRowRealizedPL,
				formatCurrencySet(perf.Currencies),
				perf.TotalTrades,
				perf.ShareVolume,
				perf.NetPeriodQuantity)
		}
	}

	fmt.Printf("\nWORST PERFORMERS (BOTTOM 20):\n")
	fmt.Printf("Symbol | Trade-Row Realized P/L | Currency | # Trades | Volume | Net Period Qty\n")
	fmt.Printf("----------------------------------------------------------------------------\n")

	length := len(performances)
	for i := 0; i < 20 && i < length; i++ {
		perf := performances[length-i-1]
		fmt.Printf("%s | %.2f | %s | %d | %.0f | %.0f\n",
			perf.Symbol,
			perf.TradeRowRealizedPL,
			formatCurrencySet(perf.Currencies),
			perf.TotalTrades,
			perf.ShareVolume,
			perf.NetPeriodQuantity)
	}

	totals := summarizeTradeRows(performances)

	fmt.Printf("\nSUMMARY STATISTICS:\n")
	fmt.Printf("Total Securities Traded: %d\n", totals.Securities)
	fmt.Printf("Total Trade-Row Realized P/L: %.2f%s\n", totals.RealizedPL, mixedCurrencySuffix(reportCurrencies))
	fmt.Printf("Total Trades: %d\n", totals.Trades)
	fmt.Printf("Total Trade-Row Commission: %.2f%s\n", totals.Commission, mixedCurrencySuffix(reportCurrencies))
	fmt.Println("Reported realized P/L already includes commissions and wash-sale adjustments; commissions are shown separately for reference.")
	fmt.Printf("Profitable Securities: %d (%.1f%%)\n",
		totals.Winners, percent(totals.Winners, totals.Securities))
	fmt.Printf("Losing Securities: %d (%.1f%%)\n",
		totals.Losers, percent(totals.Losers, totals.Securities))

	fmt.Printf("\nTOP 5 MOST ACTIVE SECURITIES:\n")
	fmt.Printf("Symbol | Volume | # Trades | Trade-Row Realized P/L | Currency\n")
	fmt.Printf("-------------------------------------------------------------\n")

	// Sort by volume
	sort.Slice(performances, func(i, j int) bool {
		if performances[i].ShareVolume == performances[j].ShareVolume {
			return performances[i].Symbol+formatCurrencySet(performances[i].Currencies) < performances[j].Symbol+formatCurrencySet(performances[j].Currencies)
		}
		return performances[i].ShareVolume > performances[j].ShareVolume
	})

	for i, perf := range performances {
		if i < 5 {
			fmt.Printf("%s | %.0f | %d | %.2f | %s\n",
				perf.Symbol,
				perf.ShareVolume,
				perf.TotalTrades,
				perf.TradeRowRealizedPL,
				formatCurrencySet(perf.Currencies))
		}
	}

	// Add wash sale period reminders
	fmt.Printf("\nWASH SALE TRACKING:\n")
	fmt.Println("• Use --wash-sale-periods to view active wash sale periods")
	fmt.Println("• Use --safe-repurchase to inspect dates outside the supplied loss-sale windows")

	if xlsxPath != "" {
		fmt.Printf("\nSpreadsheet written to %s\n", xlsxPath)
	}
	return nil
}

// sortedMarkToMarketPerformance aggregates detail rows by security, best total P/L first.
func sortedMarkToMarketPerformance(rows []activity.MarkToMarketPerformance) []MarkToMarketSecurityPerformance {
	performances := aggregateMarkToMarketPerformance(rows)
	sort.Slice(performances, func(i, j int) bool {
		if performances[i].TotalPL == performances[j].TotalPL {
			return performances[i].Symbol+performances[i].AssetCategory < performances[j].Symbol+performances[j].AssetCategory
		}
		return performances[i].TotalPL > performances[j].TotalPL
	})
	return performances
}

func printMarkToMarketPerformanceReport(records [][]string, report *activity.ActivityReport) {
	performances := sortedMarkToMarketPerformance(report.MarkToMarketPerformance)

	baseCurrency := normalizeCurrency(report.BaseCurrency)

	fmt.Printf("\n=== %s ===\n\n", markToMarketReportTitle(records))
	fmt.Printf("Scope: IBKR Mark-to-Market Performance Summary detail rows; amounts are account base currency (%s).\n", baseCurrency)
	fmt.Println("Includes stocks, equity/index options, and forex detail rows; excludes broker interest and other fee rows outside Total (All Assets).")
	fmt.Println("This is account-level mark-to-market performance, not tax-realized Form 8949 P/L.")
	fmt.Println()

	printChangeInNAVSummary(report.ChangeInNAV, baseCurrency)

	fmt.Printf("BEST PERFORMERS (TOP 20):\n")
	fmt.Printf("Symbol | Asset Category | Prior Qty | Current Qty | Position P/L | Transaction P/L | Commissions | Other | Total P/L\n")
	fmt.Printf("---------------------------------------------------------------------------------------------------------------\n")

	for i, perf := range performances {
		if i < 20 {
			printMarkToMarketPerformanceRow(perf, baseCurrency)
		}
	}

	fmt.Printf("\nWORST PERFORMERS (BOTTOM 20):\n")
	fmt.Printf("Symbol | Asset Category | Prior Qty | Current Qty | Position P/L | Transaction P/L | Commissions | Other | Total P/L\n")
	fmt.Printf("---------------------------------------------------------------------------------------------------------------\n")

	length := len(performances)
	for i := 0; i < 20 && i < length; i++ {
		printMarkToMarketPerformanceRow(performances[length-i-1], baseCurrency)
	}

	printMarkToMarketSummary(performances, report.MarkToMarketPerformance, baseCurrency)

	fmt.Printf("\nWASH SALE TRACKING:\n")
	fmt.Println("• Use --wash-sale-periods to view active wash sale periods")
	fmt.Println("• Use --safe-repurchase to inspect dates outside the supplied loss-sale windows")
}

func printChangeInNAVSummary(nav activity.ChangeInNAV, baseCurrency string) {
	if !nav.HasData() {
		return
	}

	fmt.Println("ACCOUNT NAV SUMMARY:")
	fmt.Printf("Starting NAV: %s\n", formatBaseCurrency(nav.StartingValue, baseCurrency))
	fmt.Printf("Ending NAV: %s\n", formatBaseCurrency(nav.EndingValue, baseCurrency))
	fmt.Printf("Net NAV Change: %s\n", formatBaseCurrency(nav.NetNAVChange(), baseCurrency))
	fmt.Printf("Deposits & Withdrawals: %s\n", formatBaseCurrency(nav.DepositsWithdrawals, baseCurrency))
	fmt.Printf("Position Transfers: %s\n", formatBaseCurrency(nav.PositionTransfers, baseCurrency))
	fmt.Printf("External Flows: %s\n", formatBaseCurrency(nav.ExternalFlows(), baseCurrency))
	fmt.Printf("Gain Before External Flows: %s\n", formatBaseCurrency(nav.GainBeforeExternalFlows(), baseCurrency))
	fmt.Printf("Mark-to-Market: %s\n", formatBaseCurrency(nav.MarkToMarket, baseCurrency))
	fmt.Printf("Dividends: %s\n", formatBaseCurrency(nav.Dividends, baseCurrency))
	fmt.Printf("Interest: %s\n", formatBaseCurrency(nav.Interest, baseCurrency))
	fmt.Printf("Commissions: %s\n\n", formatBaseCurrency(nav.Commissions, baseCurrency))
}

func printMarkToMarketPerformanceRow(perf MarkToMarketSecurityPerformance, baseCurrency string) {
	fmt.Printf("%s | %s | %.0f | %.0f | %s | %s | %s | %s | %s\n",
		perf.Symbol,
		perf.AssetCategory,
		perf.PriorQuantity,
		perf.CurrentQuantity,
		formatBaseCurrency(perf.PositionPL, baseCurrency),
		formatBaseCurrency(perf.TransactionPL, baseCurrency),
		formatBaseCurrency(perf.CommissionPL, baseCurrency),
		formatBaseCurrency(perf.OtherPL, baseCurrency),
		formatBaseCurrency(perf.TotalPL, baseCurrency))
}

func printMarkToMarketSummary(performances []MarkToMarketSecurityPerformance, rows []activity.MarkToMarketPerformance, baseCurrency string) {
	var total MarkToMarketAssetSummary
	var winners int
	var losers int

	for _, perf := range performances {
		total.SymbolCount++
		total.PositionPL += perf.PositionPL
		total.TransactionPL += perf.TransactionPL
		total.CommissionPL += perf.CommissionPL
		total.OtherPL += perf.OtherPL
		total.TotalPL += perf.TotalPL

		if perf.TotalPL > 0 {
			winners++
		} else if perf.TotalPL < 0 {
			losers++
		}
	}

	fmt.Printf("\nSUMMARY STATISTICS:\n")
	fmt.Printf("Total Detail Rows: %d\n", len(performances))
	fmt.Printf("Total MTM P/L: %s\n", formatBaseCurrency(total.TotalPL, baseCurrency))
	fmt.Printf("Position P/L: %s\n", formatBaseCurrency(total.PositionPL, baseCurrency))
	fmt.Printf("Transaction P/L: %s\n", formatBaseCurrency(total.TransactionPL, baseCurrency))
	fmt.Printf("Commissions: %s\n", formatBaseCurrency(total.CommissionPL, baseCurrency))
	fmt.Printf("Other: %s\n", formatBaseCurrency(total.OtherPL, baseCurrency))
	fmt.Printf("Profitable Symbols: %d (%.1f%%)\n", winners, percent(winners, len(performances)))
	fmt.Printf("Losing Symbols: %d (%.1f%%)\n", losers, percent(losers, len(performances)))

	if ibkrTotal, ok := findMarkToMarketTotal(rows, "Total (All Assets)"); ok {
		fmt.Printf("IBKR Total (All Assets): %s\n", formatBaseCurrency(ibkrTotal.TotalPL, baseCurrency))
	}

	assetSummaries := summarizeMarkToMarketByAsset(performances)
	fmt.Printf("\nASSET CATEGORY SUMMARY:\n")
	fmt.Printf("Asset Category | Symbols | Position P/L | Transaction P/L | Commissions | Other | Total P/L\n")
	fmt.Printf("-------------------------------------------------------------------------------------------\n")
	for _, summary := range assetSummaries {
		fmt.Printf("%s | %d | %s | %s | %s | %s | %s\n",
			summary.AssetCategory,
			summary.SymbolCount,
			formatBaseCurrency(summary.PositionPL, baseCurrency),
			formatBaseCurrency(summary.TransactionPL, baseCurrency),
			formatBaseCurrency(summary.CommissionPL, baseCurrency),
			formatBaseCurrency(summary.OtherPL, baseCurrency),
			formatBaseCurrency(summary.TotalPL, baseCurrency))
	}
}

func aggregateMarkToMarketPerformance(rows []activity.MarkToMarketPerformance) []MarkToMarketSecurityPerformance {
	bySecurity := make(map[string]*MarkToMarketSecurityPerformance)

	for _, row := range rows {
		if !isMarkToMarketDetail(row) {
			continue
		}

		key := row.AssetCategory + "\x00" + row.Symbol
		perf, exists := bySecurity[key]
		if !exists {
			perf = &MarkToMarketSecurityPerformance{
				AssetCategory: row.AssetCategory,
				Symbol:        row.Symbol,
			}
			bySecurity[key] = perf
		}

		perf.PriorQuantity += row.PriorQuantity
		perf.CurrentQuantity += row.CurrentQuantity
		perf.PositionPL += row.PositionPL
		perf.TransactionPL += row.TransactionPL
		perf.CommissionPL += row.CommissionPL
		perf.OtherPL += row.OtherPL
		perf.TotalPL += row.TotalPL
	}

	performances := make([]MarkToMarketSecurityPerformance, 0, len(bySecurity))
	for _, perf := range bySecurity {
		performances = append(performances, *perf)
	}

	return performances
}

func isMarkToMarketDetail(row activity.MarkToMarketPerformance) bool {
	if strings.TrimSpace(row.Symbol) == "" {
		return false
	}
	if strings.HasPrefix(row.AssetCategory, "Total") {
		return false
	}
	if row.AssetCategory == "Broker Interest Paid and Received" || row.AssetCategory == "Other Fees" {
		return false
	}
	return true
}

func summarizeMarkToMarketByAsset(performances []MarkToMarketSecurityPerformance) []MarkToMarketAssetSummary {
	byAsset := make(map[string]*MarkToMarketAssetSummary)
	for _, perf := range performances {
		summary, exists := byAsset[perf.AssetCategory]
		if !exists {
			summary = &MarkToMarketAssetSummary{AssetCategory: perf.AssetCategory}
			byAsset[perf.AssetCategory] = summary
		}

		summary.SymbolCount++
		summary.PositionPL += perf.PositionPL
		summary.TransactionPL += perf.TransactionPL
		summary.CommissionPL += perf.CommissionPL
		summary.OtherPL += perf.OtherPL
		summary.TotalPL += perf.TotalPL
	}

	summaries := make([]MarkToMarketAssetSummary, 0, len(byAsset))
	for _, summary := range byAsset {
		summaries = append(summaries, *summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].TotalPL == summaries[j].TotalPL {
			return summaries[i].AssetCategory < summaries[j].AssetCategory
		}
		return summaries[i].TotalPL > summaries[j].TotalPL
	})

	return summaries
}

func findMarkToMarketTotal(rows []activity.MarkToMarketPerformance, assetCategory string) (activity.MarkToMarketPerformance, bool) {
	for _, row := range rows {
		if row.AssetCategory == assetCategory {
			return row, true
		}
	}
	return activity.MarkToMarketPerformance{}, false
}

func markToMarketReportTitle(records [][]string) string {
	for _, record := range records {
		if len(record) >= 4 && record[0] == "Statement" && record[1] == "Data" && record[2] == "Period" {
			periodLabel := reportPeriodLabel(record[3])
			if periodLabel != "" {
				return fmt.Sprintf("%s BASE-CURRENCY MARK-TO-MARKET PERFORMANCE REPORT", periodLabel)
			}
		}
	}

	return "BASE-CURRENCY MARK-TO-MARKET PERFORMANCE REPORT"
}

func securityReportTitle(records [][]string) string {
	for _, record := range records {
		if len(record) >= 4 && record[0] == "Statement" && record[1] == "Data" && record[2] == "Period" {
			periodLabel := reportPeriodLabel(record[3])
			if periodLabel != "" {
				return fmt.Sprintf("%s STOCK TRADE-ROW PERFORMANCE REPORT", periodLabel)
			}
		}
	}

	return "STOCK TRADE-ROW PERFORMANCE REPORT"
}

func reportPeriodLabel(period string) string {
	period = strings.TrimSpace(period)
	if period == "" {
		return ""
	}

	years := reportYearPattern.FindAllString(period, -1)
	if len(years) == 0 {
		return period
	}

	firstYear := years[0]
	lastYear := years[len(years)-1]
	if firstYear == lastYear {
		return firstYear
	}

	return fmt.Sprintf("%s-%s", firstYear, lastYear)
}

func printTradeRowScope(currencies map[string]struct{}) {
	fmt.Println("Scope: stock Trades rows only; excludes options, dividends, interest, transfers, NAV, and base-currency performance summaries.")
	fmt.Println("Amounts below are raw IBKR trade-row values in each instrument's trading currency.")
	fmt.Println("Net Period Qty is buys minus sells within the statement period, not IBKR ending position when the period starts with open holdings.")
	if len(currencies) > 1 {
		fmt.Printf("Currencies present: %s. Totals add mixed currencies and are not account-level USD gains.\n\n", formatCurrencySet(currencies))
		return
	}
	if len(currencies) == 1 {
		fmt.Printf("Currency present: %s.\n\n", formatCurrencySet(currencies))
		return
	}
	fmt.Println()
}

func normalizeCurrency(currency string) string {
	currency = strings.TrimSpace(currency)
	if currency == "" {
		return "UNKNOWN"
	}
	return currency
}

func formatCurrencySet(currencies map[string]struct{}) string {
	if len(currencies) == 0 {
		return "UNKNOWN"
	}

	values := make([]string, 0, len(currencies))
	for currency := range currencies {
		values = append(values, currency)
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

func mixedCurrencySuffix(currencies map[string]struct{}) string {
	if len(currencies) == 0 {
		return ""
	}
	if len(currencies) == 1 {
		return " " + formatCurrencySet(currencies)
	}
	return fmt.Sprintf(" (mixed currencies: %s)", formatCurrencySet(currencies))
}

func formatBaseCurrency(amount float64, currency string) string {
	if currency == "" || currency == "USD" {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

func percent(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) / float64(total) * 100
}
