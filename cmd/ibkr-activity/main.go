package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
)

func main() {
	// Set up command line flags
	activityFile := flag.String("file", "", "Path to the IBKR Activity Report CSV file")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	symbol := flag.String("symbol", "", "Focus analysis on a specific symbol")
	analyzePosition := flag.Bool("analyze-position", false, "Summarize trades and statement holdings for the specified stock symbol")
	analyzeAll := flag.Bool("analyze-all", false, "Run analysis on all securities traded")
	analyzeTransaction := flag.Bool("analyze-transaction", false, "Analyze specific transactions for a symbol")
	transactionDate := flag.String("date", "", "Date to analyze in YYYY-MM-DD format (required with analyze-transaction)")
	transactionQuantity := flag.Float64("quantity", 0, "Approximate quantity to look for (optional with analyze-transaction)")
	analyzeWashSalePeriods := flag.Bool("wash-sale-periods", false, "Analyze active wash sale periods")
	safeRepurchaseDates := flag.Bool("safe-repurchase", false, "Show first dates outside the supplied loss-sale windows")
	googleCalendarLinks := flag.Bool("google-calendar-links", false, "Include Google Calendar links for window end dates (used with --safe-repurchase)")
	printAllTransactions := flag.Bool("print-all-transactions", false, "List statement trades, optionally filtered by --symbol, or add the timeline to --analyze-position")
	flag.Parse()
	*symbol = strings.ToUpper(strings.TrimSpace(*symbol))

	// Configure logger
	setupLogger(*logLevel)

	// Validate input file
	if *activityFile == "" {
		slog.Error("No activity file specified")
		flag.Usage()
		os.Exit(1)
	}
	if *printAllTransactions && (*analyzeAll || *analyzeTransaction || *analyzeWashSalePeriods || *safeRepurchaseDates) {
		slog.Error("Use --print-all-transactions by itself or with --analyze-position")
		os.Exit(1)
	}

	// Handle the wash sale periods analysis
	if *analyzeWashSalePeriods {
		err := AnalyzeWashSalePeriods(*activityFile)
		if err != nil {
			slog.Error("Failed to analyze wash sale periods", "error", err)
			os.Exit(1)
		}
		return
	}

	// Handle the safe repurchase dates analysis
	if *safeRepurchaseDates {
		err := GetSafeRepurchaseDates(*activityFile, *googleCalendarLinks)
		if err != nil {
			slog.Error("Failed to get safe repurchase dates", "error", err)
			os.Exit(1)
		}
		return
	}

	// Handle the all securities analysis
	if *analyzeAll {
		err := AnalyzeAllSecurities(*activityFile)
		if err != nil {
			slog.Error("Failed to analyze all securities", "error", err)
			os.Exit(1)
		}
		return
	}

	// Validate symbol if specified
	if (*analyzePosition || *analyzeTransaction) && *symbol == "" {
		slog.Error("Symbol is required when using analyze-position or analyze-transaction")
		flag.Usage()
		os.Exit(1)
	}

	// Handle the position analysis for a specific symbol
	if *analyzePosition {
		err := analyzeSymbolPosition(*activityFile, *symbol, *printAllTransactions)
		if err != nil {
			slog.Error("Failed to analyze position", "error", err)
			os.Exit(1)
		}
		return
	}

	// Handle transaction analysis
	if *analyzeTransaction {
		if *transactionDate == "" {
			slog.Error("Date is required when using analyze-transaction")
			flag.Usage()
			os.Exit(1)
		}

		// Parse the date
		date, err := time.Parse("2006-01-02", *transactionDate)
		if err != nil {
			slog.Error("Invalid date format, use YYYY-MM-DD", "error", err)
			os.Exit(1)
		}

		// Parse the activity report
		report, err := activity.ParseActivityReport(*activityFile)
		if err != nil {
			slog.Error("Failed to parse activity report file", "error", err)
			os.Exit(1)
		}

		// Run the transaction analysis
		AnalyzeSpecificTransaction(report, *symbol, date, *transactionQuantity)
		return
	}

	// Load the report for transaction listing or the default analysis.
	report, err := activity.ParseActivityReport(*activityFile)
	if err != nil {
		slog.Error("Failed to parse activity report file", "error", err)
		os.Exit(1)
	}
	if *printAllTransactions {
		printTransactionList(report, *symbol)
		return
	}

	slog.Info("Parsed activity report", "period", report.Period, "account", report.AccountID)
	slog.Info("Trades found", "count", len(report.Trades))

	// If a symbol was specified, provide detailed analysis for that symbol
	if *symbol != "" {
		symbol := strings.ToUpper(*symbol)
		trades, found := report.TradesBySymbol[symbol]
		if !found {
			fmt.Printf("No trades found for symbol: %s\n", symbol)
			os.Exit(0)
		}

		analysis := activity.AnalyzeSymbol(trades)
		activity.PrintSymbolAnalysis(analysis)
	} else {
		// Otherwise, print a report of all suspicious wash sales
		activity.PrintWashSaleReport(report)
	}
}

func setupLogger(level string) {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
