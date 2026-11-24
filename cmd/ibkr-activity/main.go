package main

import (
	"errors"
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
	var files statementFiles
	flag.Var(&files, "file", "Path to an IBKR activity statement CSV; repeat it, or list more paths after it, to combine statements")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	symbol := flag.String("symbol", "", "Focus analysis on a specific symbol")
	analyzePosition := flag.Bool("analyze-position", false, "Summarize trades and statement holdings for the specified stock symbol")
	analyzeAll := flag.Bool("analyze-all", false, "Run analysis on all securities traded")
	xlsxPath := flag.String("xlsx", "", "Also write the --analyze-all or --analyze-position results to this .xlsx spreadsheet (opens in Google Sheets and Excel)")
	analyzeTransaction := flag.Bool("analyze-transaction", false, "Analyze specific transactions for a symbol")
	transactionDate := flag.String("date", "", "Date to analyze in YYYY-MM-DD format (required with analyze-transaction)")
	transactionQuantity := flag.Float64("quantity", 0, "Approximate quantity to look for (optional with analyze-transaction)")
	analyzeWashSalePeriods := flag.Bool("wash-sale-periods", false, "Analyze active wash sale periods")
	safeRepurchaseDates := flag.Bool("safe-repurchase", false, "Show first dates outside the supplied loss-sale windows")
	googleCalendarLinks := flag.Bool("google-calendar-links", false, "Include Google Calendar links for window end dates (used with --safe-repurchase)")
	allowMultipleAccounts := flag.Bool("allow-multiple-accounts", false, "Combine statements from different accounts (refused by default)")
	printAllTransactions := flag.Bool("print-all-transactions", false, "List statement trades, optionally filtered by --symbol, or add the timeline to --analyze-position")
	files = append(files, parseArgs(flag.CommandLine, os.Args[1:])...)
	*symbol = strings.ToUpper(strings.TrimSpace(*symbol))
	mergeOptions.AllowMultipleAccounts = *allowMultipleAccounts

	// Configure logger
	setupLogger(*logLevel)

	// Validate input file
	if len(files) == 0 {
		slog.Error("No activity file specified")
		flag.Usage()
		os.Exit(1)
	}
	if *printAllTransactions && (*analyzeAll || *analyzeTransaction || *analyzeWashSalePeriods || *safeRepurchaseDates) {
		slog.Error("Use --print-all-transactions by itself or with --analyze-position")
		os.Exit(1)
	}

	if *xlsxPath != "" {
		if !*analyzeAll && !*analyzePosition {
			slog.Error("--xlsx is used with --analyze-all or --analyze-position")
			os.Exit(1)
		}
		if err := validSpreadsheetPath(*xlsxPath); err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
	}

	// Handle the wash sale periods analysis
	if *analyzeWashSalePeriods {
		err := AnalyzeWashSalePeriods(files)
		if err != nil {
			exitOnError("Failed to analyze wash sale periods", err)
		}
		return
	}

	// Handle the safe repurchase dates analysis
	if *safeRepurchaseDates {
		err := GetSafeRepurchaseDates(files, *googleCalendarLinks)
		if err != nil {
			exitOnError("Failed to get safe repurchase dates", err)
		}
		return
	}

	// Handle the all securities analysis
	if *analyzeAll {
		err := AnalyzeAllSecurities(files, *xlsxPath)
		if err != nil {
			exitOnError("Failed to analyze all securities", err)
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
		err := analyzeSymbolPosition(files, *symbol, *printAllTransactions, *xlsxPath)
		if err != nil {
			exitOnError("Failed to analyze position", err)
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
			exitOnError("Invalid date format, use YYYY-MM-DD", err)
		}

		// Parse the activity report
		report, err := loadStatements(files)
		if err != nil {
			exitOnError("Failed to parse activity report file", err)
		}

		// Run the transaction analysis
		AnalyzeSpecificTransaction(report, *symbol, date, *transactionQuantity)
		return
	}

	// Load the report for transaction listing or the default analysis.
	report, err := loadStatements(files)
	if err != nil {
		exitOnError("Failed to parse activity report file", err)
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
		if _, found := report.TradesBySymbol[symbol]; !found {
			fmt.Printf("No trades found for symbol: %s\n", symbol)
			os.Exit(0)
		}

		analysis := report.AnalyzeTrades(symbol)
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

// statementFiles collects every --file value.
type statementFiles []string

func (f *statementFiles) String() string { return strings.Join(*f, ", ") }

func (f *statementFiles) Set(path string) error {
	*f = append(*f, path)
	return nil
}

// parseArgs parses flags anywhere on the command line and returns the other
// arguments, so a shell glob such as --file *.csv supplies every matching file.
func parseArgs(flags *flag.FlagSet, args []string) []string {
	var extra []string
	for {
		if err := flags.Parse(args); err != nil {
			os.Exit(2)
		}
		rest := flags.Args()
		if len(rest) == 0 {
			return extra
		}
		extra = append(extra, rest[0])
		args = rest[1:]
	}
}

// mergeOptions controls how loadStatements combines several statements.
var mergeOptions activity.MergeOptions

// exitOnError reports err and exits. Explanations meant for the reader, such as
// statements from several accounts, print as written rather than as a log line.
func exitOnError(context string, err error) {
	var accounts *activity.AccountMismatchError
	if errors.As(err, &accounts) {
		fmt.Fprintln(os.Stderr, "\n"+err.Error())
	} else {
		slog.Error(context, "error", err)
	}
	os.Exit(1)
}

// loadStatements reads the statement files, combining several into one report,
// and lists the combined files.
func loadStatements(files []string) (*activity.ActivityReport, error) {
	report, err := activity.LoadActivityReports(files, mergeOptions)
	if err != nil {
		return nil, err
	}
	if len(report.Sources) > 1 {
		fmt.Printf("Combined %d statements (see warnings for overlaps and gaps):\n", len(report.Sources))
		for _, source := range report.Sources {
			fmt.Printf("  %s: %s, %d of %d trades used\n", source.Name, source.Period, source.TradesUsed, source.Trades)
		}
		fmt.Println()
	}
	return report, nil
}
