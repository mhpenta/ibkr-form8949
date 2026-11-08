package main

import (
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/form8949"
	"github.com/mhpenta/ibkr-form8949/internal/report"
)

func main() {
	// Set up command line flags
	csvFilePath := flag.String("file", "", "Path to the Interactive Brokers Form 8949 CSV file")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	reportType := flag.String("report", "standard", "Report type: 'standard', 'quarterly', 'position', 'lots', 'position-performance', or 'all'")
	year := flag.Int("year", time.Now().Year(), "Tax year for analysis")
	extendedWindow := flag.Int("extended-window", 35, "Upper limit for 'just outside window' range (31-X days)")
	maxWindow := flag.Int("max-window", 45, "Maximum window to check for nearby transactions (X days)")
	focusReport := flag.String("focus", "", "Focus report on specific issue: 'no-repurchase' for wash sales with no matching purchase")
	flag.Parse()

	// Configure logger
	setupLogger(*logLevel)

	userSpecifiedYear := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "year" {
			userSpecifiedYear = true
		}
	})
	if *year < 2000 || *year > 2100 || *extendedWindow < 30 || *maxWindow < *extendedWindow {
		slog.Error("Invalid flags: year must be 2000–2100 and 30 <= extended-window <= max-window")
		os.Exit(1)
	}

	// Validate input file
	if *csvFilePath == "" {
		slog.Error("No CSV file specified")
		flag.Usage()
		os.Exit(1)
	}

	// Process the CSV file
	transactions, csvYear, err := form8949.ParseCSVFile(*csvFilePath)
	if err != nil {
		slog.Error("Failed to parse CSV file", "error", err)
		os.Exit(1)
	}

	slog.Info("Parsed transactions", "count", len(transactions))
	slog.Info("CSV year detection", "csvYear", csvYear, "filename", *csvFilePath)

	// Debug output for the first transaction
	if len(transactions) > 0 {
		tx := transactions[0]
		slog.Debug("First transaction",
			"description", tx.Description,
			"dateAcquired", tx.DateAcquired,
			"dateSold", tx.DateSold,
			"proceeds", tx.Proceeds,
			"costBasis", tx.CostBasis,
			"isWashSale", tx.IsWashSale)
	}

	// Prioritize years in this order:
	// 1. User-specified year via command line flag
	// 2. Year found in CSV header
	// 3. Year extracted from filename
	// 4. Current year (default)

	if !userSpecifiedYear {
		// If we found a year in the CSV header, use it
		if csvYear > 0 {
			*year = csvYear
			slog.Info("Using year from CSV header", "year", *year)
		} else if *csvFilePath != "" {
			// Try to extract from filename as fallback
			baseName := filepath.Base(*csvFilePath)
			yearStr := strings.TrimSuffix(baseName, filepath.Ext(baseName))
			if extractedYear, err := strconv.Atoi(yearStr); err == nil && extractedYear >= 2000 && extractedYear <= 2100 {
				*year = extractedYear
				slog.Info("Extracted year from filename", "year", *year)
			} else {
				// Use default (current year)
				slog.Info("Using default year for analysis", "year", *year)
			}
		} else {
			// Use default (current year)
			slog.Info("Using default year for analysis", "year", *year)
		}
	} else {
		slog.Info("Using user-specified year for analysis", "year", *year)
	}

	// Analyze the transactions
	analysis := form8949.AnalyzeTransactions(transactions, *year, *extendedWindow, *maxWindow)

	if len(analysis.Transactions) == 0 {
		slog.Error("No dispositions found for selected year", "year", *year, "loaded", len(transactions))
		os.Exit(1)
	}
	fallback, unknown := 0, 0
	for _, tx := range analysis.Transactions {
		if tx.TermSource != "worksheet" {
			fallback++
		}
		if tx.TermUnknown {
			unknown++
		}
	}
	if fallback > 0 {
		slog.Warn("Holding periods lack worksheet classification; date fallback cannot include inherited holding periods", "transactions", fallback, "unknownTerm", unknown)
	}

	// Check if we need a focused report
	if *focusReport != "" {
		if report.HandleFocusReport(strings.ToLower(*focusReport), analysis) {
			return
		}
		slog.Warn("Unknown focus report type, falling back to standard report", "focusType", *focusReport)
	}

	// Print analysis results based on report type
	report.HandleReportType(strings.ToLower(*reportType), analysis)
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
