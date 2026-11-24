package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

// createGoogleCalendarLink generates an event URL for the first date outside a sale's window.
// An empty reportedPL marks a symbol with no active window in the supplied trades.
func createGoogleCalendarLink(symbol string, repurchaseDate time.Time, reportedPL string) string {
	// Format title and details for the event
	title := fmt.Sprintf("Review Repurchase Window: %s", symbol)
	details := fmt.Sprintf("No active loss-sale window for %s was found in the supplied trades. Review newer trades and other accounts before repurchasing.", symbol)
	if reportedPL != "" {
		details = fmt.Sprintf("The 30-day window for the supplied %s loss sales has ended. Review newer trades and other accounts before repurchasing.\nReported P/L: %s", symbol, reportedPL)
	}

	// Create URL with parameters
	params := url.Values{}
	params.Add("action", "TEMPLATE")
	params.Add("text", title)
	params.Add("details", details)
	params.Add("dates", fmt.Sprintf("%s/%s",
		repurchaseDate.Format("20060102"),
		repurchaseDate.AddDate(0, 0, 1).Format("20060102"))) // End date is next day
	params.Add("crm", "AVAILABLE") // Show as available

	return "https://calendar.google.com/calendar/render?" + params.Encode()
}

// AnalyzeWashSalePeriods analyzes the activity report for active wash sale periods
func AnalyzeWashSalePeriods(files []string) error {
	report, err := loadStatements(files)
	if err != nil {
		return fmt.Errorf("error parsing activity report: %w", err)
	}

	// Get the report date or use current date if not available
	reportDate := report.StatementDate
	if reportDate.IsZero() {
		reportDate = time.Now()
	}
	report.Today = reportDate

	report.PrintWindowCoverage(reportDate)
	// Get active wash sale periods, short-cover losses, and any future-dated loss trades.
	activePeriods := activity.GetActiveWashSalePeriods(report)
	shortLosses := activity.GetActiveShortCoverLosses(report)
	futureLosses := activity.GetFutureLossTrades(report)

	// Print the wash sale periods report
	activity.PrintActiveWashSaleReport(activePeriods, shortLosses, futureLosses, reportDate, activity.GetTradedSymbols(report))

	return nil
}

// GetSafeRepurchaseDates prints dates outside the supplied loss-sale windows.
func GetSafeRepurchaseDates(files []string, withGoogleCalendarLinks bool) error {
	report, err := loadStatements(files)
	if err != nil {
		return fmt.Errorf("error parsing activity report: %w", err)
	}

	return printSafeRepurchaseDatesAt(report, time.Now(), withGoogleCalendarLinks)
}

// printSafeRepurchaseDatesAt converts a wall-clock instant into the statement's
// timezone when known before evaluating active windows.
func printSafeRepurchaseDatesAt(report *activity.ActivityReport, now time.Time, withGoogleCalendarLinks bool) error {
	return printSafeRepurchaseDates(report, report.AsOfInStatementZone(now), withGoogleCalendarLinks)
}

func printSafeRepurchaseDates(report *activity.ActivityReport, comparisonDate time.Time, withGoogleCalendarLinks bool) error {
	// The caller owns the as-of date; copy to avoid changing a parsed report.
	asOfReport := *report
	asOfReport.Today = comparisonDate

	activePeriods := activity.GetActiveWashSalePeriods(&asOfReport)
	shortLosses := activity.GetActiveShortCoverLosses(&asOfReport)
	futureLosses := activity.GetFutureLossTrades(&asOfReport)

	type SymbolData struct {
		endDate time.Time
		// Reported P/L by trading currency; amounts are never converted.
		plByCurrency map[string]float64
	}

	symbolDataMap := make(map[string]*SymbolData)

	for _, period := range activePeriods {
		symbolData, exists := symbolDataMap[period.Symbol]
		if !exists {
			symbolData = &SymbolData{endDate: period.PeriodEndDate, plByCurrency: make(map[string]float64)}
			symbolDataMap[period.Symbol] = symbolData
		}
		if period.PeriodEndDate.After(symbolData.endDate) {
			symbolData.endDate = period.PeriodEndDate
		}
		symbolData.plByCurrency[period.Currency] += period.LossAmount
	}

	type shortSymbolData struct {
		latestCover  time.Time
		plByCurrency map[string]float64
	}
	shortDataMap := make(map[string]*shortSymbolData)
	for _, loss := range shortLosses {
		data, exists := shortDataMap[loss.Symbol]
		if !exists {
			data = &shortSymbolData{latestCover: loss.CoverDate, plByCurrency: make(map[string]float64)}
			shortDataMap[loss.Symbol] = data
		}
		if loss.CoverDate.After(data.latestCover) {
			data.latestCover = loss.CoverDate
		}
		data.plByCurrency[loss.Currency] += loss.LossAmount
	}

	futureSymbols := make(map[string]bool)
	for _, trade := range futureLosses {
		futureSymbols[trade.Symbol] = true
	}

	fmt.Printf("\n=== SAFE REPURCHASE DATES ===\n")
	asOfLabel := comparisonDate.Format("January 2, 2006")
	if zone, _ := comparisonDate.Zone(); zone != "" && report.StatementLocation != nil && comparisonDate.Location() == report.StatementLocation {
		asOfLabel += " (" + zone + ")"
	}
	fmt.Printf("As of: %s\n\n", asOfLabel)
	report.PrintWindowCoverage(comparisonDate)
	defer activity.PrintWashSaleScope()

	if len(symbolDataMap) == 0 && len(shortDataMap) == 0 && len(futureLosses) == 0 {
		fmt.Println("No active loss-sale windows found in the supplied trades.")
		return nil
	}

	if len(symbolDataMap) > 0 {
		// Set up table headers based on whether we're showing calendar links
		fmt.Println("First dates outside the supplied loss-sale windows:")
		if withGoogleCalendarLinks {
			fmt.Println("Symbol | Reported P/L | First Date Outside Window | Days to Wait | Google Calendar Link")
			fmt.Println("---------------------------------------------------------------------------------")
		} else {
			fmt.Println("Symbol | Reported P/L | First Date Outside Window | Days to Wait")
			fmt.Println("----------------------------------------------------------")
		}

		type safeDate struct {
			Symbol     string
			ReportedPL string
			Date       time.Time
			DaysWait   int
		}

		var safeDates []safeDate
		for symbol, data := range symbolDataMap {
			daysToWait := washsale.DaysBetween(comparisonDate, data.endDate)
			if daysToWait < 0 {
				daysToWait = 0
			}

			safeDates = append(safeDates, safeDate{
				Symbol:     symbol,
				ReportedPL: formatCurrencyAmounts(data.plByCurrency),
				Date:       data.endDate,
				DaysWait:   daysToWait,
			})
		}

		sort.Slice(safeDates, func(i, j int) bool {
			if safeDates[i].DaysWait == safeDates[j].DaysWait {
				return safeDates[i].Symbol < safeDates[j].Symbol
			}
			return safeDates[i].DaysWait < safeDates[j].DaysWait
		})

		for _, sd := range safeDates {
			fmt.Printf("%s | %s | %s | %d days", sd.Symbol, sd.ReportedPL, sd.Date.Format("01/02/2006"), sd.DaysWait)
			if withGoogleCalendarLinks {
				fmt.Printf(" | %s", createGoogleCalendarLink(sd.Symbol, sd.Date, sd.ReportedPL))
			}
			fmt.Println()
		}
	} else {
		fmt.Println("No active long loss-sale windows found in the supplied trades.")
	}

	if len(shortDataMap) > 0 {
		type shortRow struct {
			symbol      string
			latestCover time.Time
			reportedPL  string
		}
		var rows []shortRow
		for symbol, data := range shortDataMap {
			rows = append(rows, shortRow{
				symbol:      symbol,
				latestCover: data.latestCover,
				reportedPL:  formatCurrencyAmounts(data.plByCurrency),
			})
		}
		sort.Slice(rows, func(i, j int) bool {
			if !rows[i].latestCover.Equal(rows[j].latestCover) {
				return rows[i].latestCover.Before(rows[j].latestCover)
			}
			return rows[i].symbol < rows[j].symbol
		})
		fmt.Printf("\n=== SHORT-COVER LOSSES REQUIRING SEPARATE REVIEW ===\n")
		fmt.Println("Short-cover wash-sale timing (IRC §1091(e)) requires delivery and lot details.")
		fmt.Println("Symbol | Latest Cover Date | Reported P/L")
		fmt.Println("-----------------------------------------")
		for _, row := range rows {
			fmt.Printf("%s | %s | %s\n", row.symbol, row.latestCover.Format("01/02/2006"), row.reportedPL)
		}
	}

	activity.PrintFutureLossTrades(futureLosses)

	allSymbols := make(map[string]bool)
	for symbol := range report.TradesBySymbol {
		allSymbols[symbol] = true
	}

	fmt.Printf("\n=== SECURITIES WITH NO ACTIVE WINDOW IN THIS REPORT ===\n")
	var safeSymbols []string
	for symbol := range allSymbols {
		if _, exists := symbolDataMap[symbol]; exists {
			continue
		}
		if _, exists := shortDataMap[symbol]; exists {
			continue
		}
		if futureSymbols[symbol] {
			continue
		}
		safeSymbols = append(safeSymbols, symbol)
	}

	if len(safeSymbols) > 0 {
		sort.Strings(safeSymbols)
		for _, symbol := range safeSymbols {
			if withGoogleCalendarLinks {
				link := createGoogleCalendarLink(symbol, comparisonDate, "")
				fmt.Printf("• %s - No active window found in supplied trades | %s\n", symbol, link)
			} else {
				fmt.Printf("• %s - No active window found in supplied trades\n", symbol)
			}
		}
	} else {
		fmt.Println("All traded securities with recent sales at a loss or losses requiring review are listed above.")
	}

	return nil
}

// formatCurrencyAmounts lists one amount per trading currency, in currency order.
func formatCurrencyAmounts(byCurrency map[string]float64) string {
	currencies := make([]string, 0, len(byCurrency))
	for currency := range byCurrency {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	parts := make([]string, len(currencies))
	for i, currency := range currencies {
		parts[i] = activity.FormatAmount(byCurrency[currency], currency)
	}
	return strings.Join(parts, " + ")
}
