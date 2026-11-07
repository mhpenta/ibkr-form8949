package main

import (
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

// createGoogleCalendarLink generates an event URL for the first date outside a sale's window.
func createGoogleCalendarLink(symbol string, repurchaseDate time.Time, lossAmount float64) string {
	// Format title and details for the event
	title := fmt.Sprintf("Review Repurchase Window: %s", symbol)
	details := fmt.Sprintf("The 30-day window for the supplied %s loss sales has ended. Review newer trades and other accounts before repurchasing.\nReported P/L: $%.2f", symbol, lossAmount)

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
func AnalyzeWashSalePeriods(csvFilePath string) error {
	// Parse the activity report
	report, err := activity.ParseActivityReport(csvFilePath)
	if err != nil {
		return fmt.Errorf("error parsing activity report: %w", err)
	}

	// Get the report date or use current date if not available
	reportDate := report.StatementDate
	if reportDate.IsZero() {
		reportDate = time.Now()
	}
	report.Today = reportDate

	report.PrintCoverage(reportDate)
	// Get active wash sale periods
	activePeriods := activity.GetActiveWashSalePeriods(report)

	// Print the wash sale periods report
	activity.PrintActiveWashSalePeriods(activePeriods, reportDate, activity.GetTradedSymbols(report))

	return nil
}

// GetSafeRepurchaseDates prints dates outside the supplied loss-sale windows.
func GetSafeRepurchaseDates(csvFilePath string, withGoogleCalendarLinks bool) error {
	report, err := activity.ParseActivityReport(csvFilePath)
	if err != nil {
		return fmt.Errorf("error parsing activity report: %w", err)
	}

	return printSafeRepurchaseDates(report, time.Now(), withGoogleCalendarLinks)
}

func printSafeRepurchaseDates(report *activity.ActivityReport, comparisonDate time.Time, withGoogleCalendarLinks bool) error {
	// The caller owns the as-of date; copy to avoid changing a parsed report.
	asOfReport := *report
	asOfReport.Today = comparisonDate

	activePeriods := activity.GetActiveWashSalePeriods(&asOfReport)

	type SymbolData struct {
		endDate   time.Time
		lossTotal float64
	}

	symbolDataMap := make(map[string]SymbolData)

	for _, period := range activePeriods {
		symbolData, exists := symbolDataMap[period.Symbol]
		if !exists {
			symbolData = SymbolData{
				endDate:   period.PeriodEndDate,
				lossTotal: period.LossAmount,
			}
		} else {
			if period.PeriodEndDate.After(symbolData.endDate) {
				symbolData.endDate = period.PeriodEndDate
			}
			symbolData.lossTotal += period.LossAmount
		}
		symbolDataMap[period.Symbol] = symbolData
	}

	fmt.Printf("\n=== SAFE REPURCHASE DATES ===\n")
	fmt.Printf("As of: %s\n\n", comparisonDate.Format("January 2, 2006"))
	report.PrintCoverage(comparisonDate)
	defer activity.PrintWashSaleScope()

	if len(symbolDataMap) == 0 {
		fmt.Println("No active loss-sale windows found in the supplied trades.")
		return nil
	}

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
		Symbol    string
		LossTotal float64
		Date      time.Time
		DaysWait  int
	}

	var safeDates []safeDate
	for symbol, data := range symbolDataMap {
		daysToWait := washsale.DaysBetween(comparisonDate, data.endDate)
		if daysToWait < 0 {
			daysToWait = 0
		}

		safeDates = append(safeDates, safeDate{
			Symbol:    symbol,
			LossTotal: data.lossTotal,
			Date:      data.endDate,
			DaysWait:  daysToWait,
		})
	}

	sort.Slice(safeDates, func(i, j int) bool {
		if safeDates[i].DaysWait == safeDates[j].DaysWait {
			return safeDates[i].Symbol < safeDates[j].Symbol
		}
		return safeDates[i].DaysWait < safeDates[j].DaysWait
	})

	for _, sd := range safeDates {
		fmt.Printf("%s | $%.2f | %s | %d days", sd.Symbol, sd.LossTotal, sd.Date.Format("01/02/2006"), sd.DaysWait)
		if withGoogleCalendarLinks {
			fmt.Printf(" | %s", createGoogleCalendarLink(sd.Symbol, sd.Date, sd.LossTotal))
		}
		fmt.Println()
	}

	allSymbols := make(map[string]bool)
	for symbol := range report.TradesBySymbol {
		allSymbols[symbol] = true
	}

	fmt.Printf("\n=== SECURITIES WITH NO ACTIVE WINDOW IN THIS REPORT ===\n")
	var safeSymbols []string
	for symbol := range allSymbols {
		if _, exists := symbolDataMap[symbol]; !exists {
			safeSymbols = append(safeSymbols, symbol)
		}
	}

	if len(safeSymbols) > 0 {
		sort.Strings(safeSymbols)
		for _, symbol := range safeSymbols {
			if withGoogleCalendarLinks {
				link := createGoogleCalendarLink(symbol, comparisonDate, 0)
				fmt.Printf("• %s - No active window found in supplied trades | %s\n", symbol, link)
			} else {
				fmt.Printf("• %s - No active window found in supplied trades\n", symbol)
			}
		}
	} else {
		fmt.Println("All traded securities with recent sales at a loss are listed above.")
	}

	return nil
}
