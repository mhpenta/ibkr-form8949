package activity

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

// WashSalePeriod represents a wash sale period for a security
type WashSalePeriod struct {
	Symbol        string
	SaleDate      time.Time
	PeriodEndDate time.Time // First calendar date outside this sale's window.
	LossAmount    float64
	Quantity      float64
	TradePrice    float64
	Currency      string // Trading currency of LossAmount and TradePrice
}

// ShortCoverLoss represents a buy-to-cover at a loss (or with an LD code)
// within the 30-day lookback window. Under IRC §1091(e), short-sale wash-sale
// timing requires separate review of delivery and lot details.
type ShortCoverLoss struct {
	Symbol     string
	CoverDate  time.Time
	LossAmount float64 // Reported P/L; may include disallowed loss.
	Quantity   float64
	TradePrice float64
	Currency   string
}

// FormatAmount labels an amount with its trading currency. USD, or an unknown
// currency, keeps the $ prefix; other currencies are not converted.
func FormatAmount(amount float64, currency string) string {
	if currency == "" || currency == "USD" {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// GetActiveWashSalePeriods returns a list of active wash sale periods based on the activity report
func GetActiveWashSalePeriods(report *ActivityReport) []WashSalePeriod {
	var activePeriods []WashSalePeriod
	today := report.Today

	// Default to today if report date is not available
	if today.IsZero() {
		today = time.Now()
	}

	// Process each symbol's trades
	for symbol, trades := range report.TradesBySymbol {
		symbolPeriods := getSymbolWashSalePeriods(symbol, trades, today)
		activePeriods = append(activePeriods, symbolPeriods...)
	}

	// Sort periods by end date (soonest first)
	sort.Slice(activePeriods, func(i, j int) bool {
		a, b := activePeriods[i], activePeriods[j]
		if a.PeriodEndDate.Equal(b.PeriodEndDate) {
			if a.Symbol != b.Symbol {
				return a.Symbol < b.Symbol
			}
			if !a.SaleDate.Equal(b.SaleDate) {
				return a.SaleDate.Before(b.SaleDate)
			}
			if a.LossAmount != b.LossAmount {
				return a.LossAmount < b.LossAmount
			}
			return a.Quantity < b.Quantity
		}
		return a.PeriodEndDate.Before(b.PeriodEndDate)
	})

	return activePeriods
}

// GetTradedSymbols returns all symbols present in the activity report.
func GetTradedSymbols(report *ActivityReport) []string {
	var symbols []string
	for symbol := range report.TradesBySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func getSafeSymbols(allSymbols []string, activePeriods []WashSalePeriod) []string {
	activeSymbols := make(map[string]bool)
	for _, period := range activePeriods {
		activeSymbols[period.Symbol] = true
	}

	var safe []string
	for _, symbol := range allSymbols {
		if !activeSymbols[symbol] {
			safe = append(safe, symbol)
		}
	}
	sort.Strings(safe)
	return safe
}

// getSymbolWashSalePeriods identifies all active wash sale periods for a specific symbol
func getSymbolWashSalePeriods(symbol string, trades []Trade, date time.Time) []WashSalePeriod {
	var periods []WashSalePeriod

	// Include calendar days 0 through 30; day 31 is outside the window.
	for _, trade := range trades {
		// LD or a losing closed lot can leave the reported P/L zero or positive.
		if !trade.IsLossSale() {
			continue
		}

		daysSinceSale := washsale.DaysBetween(trade.DateTime, date)
		if daysSinceSale < 0 || daysSinceSale > 30 {
			continue
		}

		periodEndDate := washsale.RepurchaseDate(trade.DateTime)

		// Create a wash sale period
		period := WashSalePeriod{
			Symbol:        symbol,
			SaleDate:      trade.DateTime,
			PeriodEndDate: periodEndDate,
			LossAmount:    trade.RealizedPL, // Reported P/L may include disallowed loss.
			Quantity:      math.Abs(trade.Quantity),
			TradePrice:    trade.TradePrice,
			Currency:      trade.Currency,
		}

		periods = append(periods, period)
	}

	return periods
}

// GetActiveShortCoverLosses returns buy-to-cover losses (or LD-coded covers)
// within 0..30 calendar days of report.Today.
func GetActiveShortCoverLosses(report *ActivityReport) []ShortCoverLoss {
	today := report.Today
	if today.IsZero() {
		today = time.Now()
	}

	var losses []ShortCoverLoss
	for symbol, trades := range report.TradesBySymbol {
		for _, trade := range trades {
			if !trade.IsShortCoverLoss() {
				continue
			}
			daysSinceCover := washsale.DaysBetween(trade.DateTime, today)
			if daysSinceCover < 0 || daysSinceCover > 30 {
				continue
			}
			losses = append(losses, ShortCoverLoss{
				Symbol:     symbol,
				CoverDate:  trade.DateTime,
				LossAmount: trade.RealizedPL,
				Quantity:   math.Abs(trade.Quantity),
				TradePrice: trade.TradePrice,
				Currency:   trade.Currency,
			})
		}
	}

	sort.Slice(losses, func(i, j int) bool {
		a, b := losses[i], losses[j]
		if !a.CoverDate.Equal(b.CoverDate) {
			return a.CoverDate.Before(b.CoverDate)
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.LossAmount != b.LossAmount {
			return a.LossAmount < b.LossAmount
		}
		return a.Quantity < b.Quantity
	})

	return losses
}

// GetFutureLossTrades returns loss sales and short-cover losses dated after
// report.Today (for example, due to an exchange or timezone offset).
func GetFutureLossTrades(report *ActivityReport) []Trade {
	today := report.Today
	if today.IsZero() {
		today = time.Now()
	}

	var future []Trade
	for _, trades := range report.TradesBySymbol {
		for _, trade := range trades {
			if !trade.IsLossSale() && !trade.IsShortCoverLoss() {
				continue
			}
			if washsale.DaysBetween(trade.DateTime, today) < 0 {
				future = append(future, trade)
			}
		}
	}

	sort.Slice(future, func(i, j int) bool {
		a, b := future[i], future[j]
		if !a.DateTime.Equal(b.DateTime) {
			return a.DateTime.Before(b.DateTime)
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if a.RealizedPL != b.RealizedPL {
			return a.RealizedPL < b.RealizedPL
		}
		return math.Abs(a.Quantity) < math.Abs(b.Quantity)
	})

	return future
}

// PrintActiveWashSalePeriods prints a report of active wash sale periods.
func PrintActiveWashSalePeriods(periods []WashSalePeriod, reportDate time.Time, allSymbols []string) {
	PrintActiveWashSaleReport(periods, nil, nil, reportDate, allSymbols)
}

// PrintActiveWashSaleReport prints active long wash-sale windows alongside any
// recent short-cover losses or loss trades dated after reportDate.
func PrintActiveWashSaleReport(periods []WashSalePeriod, shortLosses []ShortCoverLoss, futureLosses []Trade, reportDate time.Time, allSymbols []string) {
	fmt.Printf("\n=== ACTIVE WASH SALE PERIODS REPORT ===\n")
	fmt.Printf("As of: %s\n\n", reportDate.Format("January 2, 2006"))
	defer PrintWashSaleScope()

	if len(periods) == 0 && len(shortLosses) == 0 && len(futureLosses) == 0 {
		fmt.Println("No active loss-sale windows found in the supplied trades.")
		return
	}

	if len(periods) > 0 {
		fmt.Printf("Found %d active wash sale periods:\n\n", len(periods))

		fmt.Println("SECURITIES WITH ACTIVE WASH SALE PERIODS:")
		fmt.Println("Symbol | Sale Date | First Date Outside Window | Days Remaining | Reported P/L | Sale Quantity | Sale Price")
		fmt.Println("-----------------------------------------------------------------------------------------")

		// Group by symbol for easier reading
		symbolGroups := make(map[string][]WashSalePeriod)
		for _, period := range periods {
			symbolGroups[period.Symbol] = append(symbolGroups[period.Symbol], period)
		}

		type symbolGroup struct {
			symbol           string
			maxDaysRemaining int
			periods          []WashSalePeriod
		}
		groups := make([]symbolGroup, 0, len(symbolGroups))
		for symbol, periods := range symbolGroups {
			sort.SliceStable(periods, func(i, j int) bool {
				return periods[i].PeriodEndDate.Before(periods[j].PeriodEndDate)
			})
			latestEndDate := periods[len(periods)-1].PeriodEndDate
			groups = append(groups, symbolGroup{
				symbol:           symbol,
				maxDaysRemaining: max(0, washsale.DaysBetween(reportDate, latestEndDate)),
				periods:          periods,
			})
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].maxDaysRemaining == groups[j].maxDaysRemaining {
				return groups[i].symbol < groups[j].symbol
			}
			return groups[i].maxDaysRemaining < groups[j].maxDaysRemaining
		})

		for _, group := range groups {
			for _, period := range group.periods {
				daysRemaining := max(0, washsale.DaysBetween(reportDate, period.PeriodEndDate))

				fmt.Printf("%s | %s | %s | %d days | %s | %.0f | %s\n",
					period.Symbol,
					period.SaleDate.Format("01/02/2006"),
					period.PeriodEndDate.Format("01/02/2006"),
					daysRemaining,
					FormatAmount(period.LossAmount, period.Currency),
					period.Quantity,
					FormatAmount(period.TradePrice, period.Currency))
			}
		}
	} else {
		fmt.Println("No active long loss-sale windows found in the supplied trades.")
	}

	if len(shortLosses) > 0 {
		fmt.Printf("\n=== SHORT-COVER LOSSES REQUIRING SEPARATE REVIEW ===\n")
		fmt.Println("Short-cover wash-sale timing (IRC §1091(e)) requires delivery and lot details.")
		fmt.Println("Symbol | Cover Date | Reported P/L | Cover Quantity | Cover Price")
		fmt.Println("-----------------------------------------------------------------")
		for _, loss := range shortLosses {
			fmt.Printf("%s | %s | %s | %g | %s\n",
				loss.Symbol,
				loss.CoverDate.Format("01/02/2006"),
				FormatAmount(loss.LossAmount, loss.Currency),
				loss.Quantity,
				FormatAmount(loss.TradePrice, loss.Currency))
		}
	}

	PrintFutureLossTrades(futureLosses)

	// Print safe securities (excluding active long windows, short-cover losses, and future loss trades).
	fmt.Printf("\n=== SECURITIES WITH NO ACTIVE WINDOW IN THIS REPORT ===\n")

	excluded := make(map[string]bool)
	for _, period := range periods {
		excluded[period.Symbol] = true
	}
	for _, loss := range shortLosses {
		excluded[loss.Symbol] = true
	}
	for _, trade := range futureLosses {
		excluded[trade.Symbol] = true
	}
	var safeSymbols []string
	for _, symbol := range allSymbols {
		if !excluded[symbol] {
			safeSymbols = append(safeSymbols, symbol)
		}
	}
	sort.Strings(safeSymbols)

	if len(safeSymbols) > 0 {
		for _, symbol := range safeSymbols {
			fmt.Printf("• %s - No active window found in supplied trades\n", symbol)
		}
	} else if len(allSymbols) == 0 {
		fmt.Println("No traded symbol list was provided.")
	} else {
		fmt.Println("All supplied symbols have an active loss-sale window or loss requiring review.")
	}
}

// PrintFutureLossTrades warns about loss trades dated after the comparison date.
func PrintFutureLossTrades(futureLosses []Trade) {
	if len(futureLosses) == 0 {
		return
	}
	fmt.Printf("\n=== LOSS TRADES DATED AFTER AS-OF DATE ===\n")
	fmt.Println("These loss trades are dated after the comparison date (for example, due to an exchange or timezone offset) and require review:")
	fmt.Println("Symbol | Trade Date | Type | Reported P/L")
	fmt.Println("-----------------------------------------")
	for _, trade := range futureLosses {
		kind := "Loss sale"
		if trade.IsShortCoverLoss() {
			kind = "Short-cover loss"
		}
		fmt.Printf("%s | %s | %s | %s\n",
			trade.Symbol,
			trade.DateTime.Format("01/02/2006"),
			kind,
			FormatAmount(trade.RealizedPL, trade.Currency))
	}
}

// PrintWashSaleScope states the limits of the repurchase-window inspection.
func PrintWashSaleScope() {
	fmt.Println("\nOnly supplied trades are checked. Missing trades, other accounts, substantially identical securities,")
	fmt.Println("short positions, and loss lots inside net profitable orders when the export has no closed-lot rows, require separate review.")
	fmt.Println("Reported P/L may already include wash-sale adjustments. These dates do not validate earlier wash sales.")
}
