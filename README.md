# ibkr-form8949

A command-line tool for analyzing Interactive Brokers Form 8949 CSV files and Activity Reports. This tool helps identify suspicious wash sales that may be incorrectly marked in your brokerage data and provides comprehensive trading performance analysis.

> [!IMPORTANT]
> **Not tax, legal, or financial advice.** ibkr-form8949 is an independent inspection tool. It
> is not affiliated with or endorsed by Interactive Brokers, and it does not prepare tax
> returns or recalculate cost basis. It can only see the files you give it, and its output may
> be incomplete or wrong. Check anything it flags against your broker's official tax documents,
> and consult a qualified tax professional before filing or changing a return. Use it at your
> own risk; see [LICENSE](LICENSE).

> [!NOTE]
> **Your data stays on your machine.** The tool reads local CSV files and makes no network
> requests. Brokerage statements contain account numbers and your full trading history, so
> never attach them to issues or pull requests. Reproduce problems with made-up trades instead.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Form 8949 Analysis Tool](#form-8949-analysis-tool)
- [Activity Report Analysis Tool](#activity-report-analysis-tool)
- [Understanding Reports](#understanding-reports)
- [Code Structure](#code-structure)
- [Development and Testing](#development-and-testing)
- [License](#license)

## Features

- **Tax Analysis**: Analyzes Interactive Brokers Form 8949 CSV files for tax reporting
- **Trading Analysis**: Processes IBKR Activity Reports for comprehensive trading performance analysis
- **Wash Sale Review**: Flags adjustments without supporting replacement evidence in the supplied records
- **Wash Sale Periods**: Tracks supplied loss-sale windows and reports statement coverage limitations
- **Options Analysis**: Examines options transactions including premiums, cost basis, and expiration outcomes
- **Multiple Report Types**: Supports different report formats for various analysis needs
- **Performance Metrics**: Calculates key trading performance metrics (hit rates, profit factors)
- **Portfolio Insights**: Provides detailed breakdowns of trading activity and performance

## Installation

### Install from Repository

```bash
go install github.com/mhpenta/ibkr-form8949@latest
```

### Build from Source

```bash
git clone https://github.com/mhpenta/ibkr-form8949.git
cd ibkr-form8949
make build
```

## Form 8949 Analysis Tool

The Form 8949 analysis tool is designed to process tax reporting files from Interactive Brokers and identify potential issues with wash sale designations.

### Tool Schema

```
COMMAND: ./ibkr-form8949
INPUT FILE: Interactive Brokers Form 8949 CSV file
PRIMARY USE CASES: Tax analysis, wash sale detection, trading performance
```

### Required Parameters

| Parameter | Description | Example |
|-----------|-------------|---------|
| `--file` | Path to the Interactive Brokers Form 8949 CSV file | `--file 2024.csv` |

### Optional Parameters

| Parameter | Description | Default | Example |
|-----------|-------------|---------|---------|
| `--year` | Tax year for analysis; explicit values always take precedence | Worksheet title, year-only filename, then current year | `--year 2024` |
| `--report` | Report type | `standard` | `--report quarterly` |
| `--focus` | Focus report on specific issue | None | `--focus no-repurchase` |
| `--extended-window` | Upper limit for 'just outside window' range (31-X days) | 35 | `--extended-window 40` |
| `--max-window` | Maximum window to check for nearby transactions (X days) | 45 | `--max-window 60` |
| `--log-level` | Log level | `info` | `--log-level debug` |

### Valid Report Types

- `standard`: Standard analysis with summary and suspicious transactions
- `quarterly`: Breaks down gains/losses by calendar quarter
- `position`: Analyzes performance at the position (security) level
- `lots`: Detailed analysis of individual trading lots
- `position-performance`: Comprehensive trading performance metrics
- `all`: Generates all report types

### Example Commands

```bash
# Basic usage
./ibkr-form8949 --file your_form8949.csv

# Focus on wash sales with no matching repurchase
./ibkr-form8949 --file 2024.csv --focus no-repurchase

# Generate quarterly analysis
./ibkr-form8949 --file 2024.csv --report quarterly

# Generate position performance analysis
./ibkr-form8949 --file 2024.csv --report position-performance

# Use a specific tax year
./ibkr-form8949 --file trading_data.csv --year 2024
```

The worksheet parser accepts quoted CSV, CRLF line endings, repeated headers and reordered columns. Malformed transaction rows stop processing with a line-numbered error; partial totals are not printed. An empty worksheet or a selected year with no dispositions also fails explicitly. Window flags must satisfy `30 <= extended-window <= max-window`.

Recognized `Part,I` / `Part,II` and `Box,A` through `Box,F` records supply the holding-period classification until the next section. Conflicting metadata is rejected. Without section metadata, dated lots use a sale strictly after the one-year anniversary; this fallback cannot infer inherited wash-sale holding periods and produces a warning. `VARIOUS` acquisitions without a section remain **unknown term**, with separate totals. Their dates are never replaced with invented dates.

## Activity Report Analysis Tool

The Activity Report analysis tool processes comprehensive IBKR activity reports to provide trading insights and investigate specific securities.

### Building the Activity Report Tool

```bash
cd ibkr-form8949
make activity
```

### Tool Schema

```
COMMAND: ./ibkr-activity
INPUT FILE: Interactive Brokers Activity Report CSV file
PRIMARY USE CASES: Portfolio analysis, security analysis, wash sale investigation
```

### Required Parameters

| Parameter | Description | Example |
|-----------|-------------|---------|
| `--file` | Path to the IBKR Activity Report CSV file | `--file activity.2024.csv` |

### Optional Parameters

| Parameter | Description | Default | Example |
|-----------|-------------|---------|---------|
| `--symbol` | Focus analysis on a specific symbol | None | `--symbol AAPL` |
| `--analyze-position` | Summarize trades and statement holdings for a stock symbol | `false` | `--analyze-position` |
| `--analyze-all` | Run analysis on all securities traded | `false` | `--analyze-all` |
| `--analyze-transaction` | Analyze specific transactions for a symbol | `false` | `--analyze-transaction` |
| `--wash-sale-periods` | Analyze active wash sale periods | `false` | `--wash-sale-periods` |
| `--safe-repurchase` | Show first dates outside the supplied loss-sale windows | `false` | `--safe-repurchase` |
| `--google-calendar-links` | Include calendar links for those dates | `false` | `--google-calendar-links` |
| `--date` | Date to analyze in YYYY-MM-DD format | | `--date 2024-06-14` |
| `--quantity` | Approximate quantity to look for | 0 | `--quantity 5590` |
| `--print-all-transactions` | List statement trades, optionally filtered by `--symbol`, or add the timeline to `--analyze-position` | `false` | `--print-all-transactions` |
| `--log-level` | Log level | `info` | `--log-level debug` |

### Example Commands

```bash
# Generate complete portfolio performance report
./ibkr-activity --file activity.2024.csv --analyze-all

# Analyze a specific security
./ibkr-activity --file activity.2024.csv --symbol AAPL

# List transactions for a specific symbol. Omit --symbol for all symbols.
./ibkr-activity --file activity.2024.csv --symbol AAPL --print-all-transactions

# Run full position analysis for a symbol
./ibkr-activity --file activity.2024.csv --symbol AAPL --analyze-position

# Detailed analysis of specific transactions on a date
./ibkr-activity --file activity.2024.csv --symbol AAPL --analyze-transaction --date 2024-06-14

# Search for specific transaction quantity
./ibkr-activity --file activity.2024.csv --symbol AAPL --analyze-transaction --date 2024-06-14 --quantity 100

# Analyze options transactions 
./ibkr-activity --file activity.2024.csv --symbol "AAPL 20SEP24 200 P"

# Get a list of active wash sale periods
./ibkr-activity --file activity.2024.csv --wash-sale-periods

# Show first dates outside the loss-sale windows found in this statement
./ibkr-activity --file activity.2024.csv --safe-repurchase

# Include Google Calendar links for those dates
./ibkr-activity --file activity.2024.csv --safe-repurchase --google-calendar-links
```

All activity commands use the same parser. Each `Trades,Header` controls the rows following it, including Forex layouts and an optional Account column. `Order` and execution-level `Trade` records are supported; `ClosedLot`, `Lot` and total rows are excluded from transaction counts. When both orders and executions are supplied for an account/security/currency, their daily quantities and monetary totals must agree; matching executions are then excluded to prevent double counting. Unknown detail levels and malformed transaction values stop processing. A missing Code column produces a warning that broker wash-sale flags are unavailable.

Symbols are case-insensitive in every command. Date-only trades are accepted, but ambiguous same-day ordering requires lot details for replacement matching. Position holdings come from the broker's ending quantity snapshot; net period purchases minus sales alone do not establish current holdings.

`--analyze-position` can show holdings from the Mark-to-Market Performance Summary for a stock with no trades in the period. Missing ending quantities are reported as unknown, and offsetting open holdings are shown with their net quantity. Ambiguous same-day buys appear separately from confirmed post-sale transactions. Short-cover losses are reported separately from long-sale windows because short-sale wash-sale timing requires delivery and lot details.

## Understanding Reports

### Wash Sale Classification

The tool classifies suspicious wash sales into these categories:

| Type | Description | Significance |
|------|-------------|-------------|
| No matching purchase | No repurchase within IRS 30-day window | Potentially incorrectly marked wash sale |
| Just outside 30-day window | Repurchases 31-35 days from sale | Borderline cases that might warrant review |
| Outside window but in range | Repurchases 36-45 days from sale | Less concerning but still worth noting |
| Zero loss wash sales | Marked as wash sale but proceeds equal basis before adjustments | Review adjustment details; zero adjusted gain/loss is normal for a fully disallowed loss |

The Form 8949 inspector matches security descriptions after removing explicit share-count prefixes or suffixes (such as `100 sh XYZ` or `XYZ 100 sh`). Option expiration, strike and type remain distinct. Acquisitions from every year supplied in the file are available for matching; totals and flagged sales remain limited to `--year`. Unknown (`VARIOUS`) acquisition dates are not used as purchase evidence.

Form 8949 contains disposed lots, not all purchases. Replacement shares still held, other accounts, and adjacent-year dispositions absent from the input cannot be checked. A missing match is a review item, not proof of a broker error; a matching acquisition does not validate the disallowed quantity or amount.

Activity statements use IBKR's `LD` code for wash-sale adjustments. Their reported realized P/L already includes commissions and disallowed losses, so commissions are not deducted a second time and a zero adjusted P/L is not treated as an invalid wash sale. Position and transaction reports use that reported P/L consistently. Replacement quantities are estimated from the supplied long trades using FIFO, separately by account, asset category, currency and symbol. Shares consumed by the sale, prior dispositions, buy-to-cover trades, and quantities already assigned to earlier losses are excluded. Partial replacement quantities, incomplete inventory, short positions and other tax-lot elections require broker closed-lot details. A reconciled broker opening/closing quantity can establish a later flat position and allow subsequent FIFO matching to resume; opening quantity alone is never invented as a dated acquisition. This is an inspection tool, not a tax-lot or basis recalculation engine.

Repurchase windows use the calendar dates written on the statement: day 30 is included and day 31 is the first date outside that sale's window. Transaction investigation lists purchases from day -30 through day +30, including the sale date, as candidates requiring lot review. Form 8949 lots disposed of on the sale date cannot by themselves establish a surviving replacement.

`--wash-sale-periods` uses the statement generation date (today when unavailable); `--safe-repurchase` uses today's local calendar date. Both print actual statement coverage and warn when the comparison date falls outside it. The generation date does not extend statement coverage. The printed dates and Google Calendar links describe the supplied sales only, and do not establish that repurchasing today is safe. Missing trades and individual loss lots hidden by net profitable orders can affect the result.

References: [IRS wash-sale rules](https://www.irs.gov/publications/p550), [Form 8949 instructions](https://www.irs.gov/instructions/i8949), [IBKR activity codes](https://www.ibkrguides.com/reportingreference/reportguide/codes_realized.htm), and [IBKR realized P/L treatment](https://www.ibkrguides.com/reportingreference/reportguide/trades_default.htm).

### Performance Metrics

| Metric | Description | Interpretation |
|--------|-------------|---------------|
| Hit Rate | Percentage of profitable trades | Higher is better |
| Profit Factor | Gross profits ÷ gross losses | >1 is profitable, >2 is strong |
| Average Win | Average gain on winning trades | Compare to average loss |
| Average Loss | Average loss on losing trades | Compare to average win |
| P/L per Trade | Average P/L across all trades | Overall efficiency metric |

Consolidated averages and profit factors use the actual gross winning and losing amounts. A positive gain with no losses has profit factor `+Inf`. Break-even trades are included in the total count but are neither wins nor losses. Lot reports count actual short-, long- and unknown-term transactions; `VARIOUS` is a separate overlapping subset. Monthly figures use actual sale dates. Calendar-quarter reports summarize realized gains/losses and do not calculate estimated tax payments.

### Activity Report Output Format

The Activity Report analysis provides:

1. **Best/Worst Performers**: Securities ranked by P/L
2. **Position Status**: Whether positions were fully liquidated
3. **Trading Volume**: Number of shares traded
4. **Transaction Timeline**: Chronological view of trades
5. **Summary Statistics**: Overall portfolio metrics
6. **Options Analysis**: Cost basis and proceeds for options trades, including expired contracts
7. **Wash Sale Periods**: List of securities still in active wash sale periods
8. **Window End Dates**: First dates outside the supplied loss-sale windows, with coverage caveats
9. **Google Calendar Links**: Optional links to add those dates to your Google Calendar

### Sample Output Structure

```
=== SECURITY PERFORMANCE REPORT ===

BEST PERFORMERS (TOP 20):
Symbol | P/L | # Trades | Volume | Remaining

WORST PERFORMERS (BOTTOM 20):
Symbol | P/L | # Trades | Volume | Remaining

SUMMARY STATISTICS:
Total Securities Traded: [number]
Total Trade-Row Realized P/L: [amount] [currency]
Total Trades: [number]
Total Trade-Row Commission: [amount] [currency]
Reported realized P/L already includes commissions and wash-sale adjustments; commissions are shown separately for reference.
Profitable Securities: [number] ([percentage]%)
Losing Securities: [number] ([percentage]%)

TOP 5 MOST ACTIVE SECURITIES:
Symbol | Volume | # Trades | P/L
```

## Code Structure

```
ibkr-form8949/
├── main.go                  # Entry point for Form 8949 analysis
├── internal/                # Core functionality
│   ├── types/               # Data structures
│   │   └── types.go         # Type definitions
│   ├── report/              # Report generators
│   │   ├── common.go        # Shared functionality
│   │   ├── standard.go      # Standard report
│   │   ├── quarterly.go     # Quarterly analysis
│   │   ├── position.go      # Position analysis
│   │   └── ... 
│   ├── activity/            # Activity report analysis
│   │   ├── parser.go        # Activity report parser
│   │   └── analysis.go      # Activity analysis logic
│   ├── analyzer.go          # Transaction analysis
│   └── parser.go            # Form 8949 parser
└── cmd/                     # Command-line applications
    └── activity/            # Activity report tool
        ├── main.go               # Entry point
        ├── transaction_analyzer.go # Transaction analysis
        ├── position_analyzer.go  # Position analysis
        ├── security_summary.go   # Multi-security analysis
        └── ... 
```

## Development and Testing

### Building the Main Application

```bash
make main
```

### Building the Activity Report Tool

```bash
make activity
```

### Running Tests

```bash
make test
```

The regression suite covers malformed input, calendar holding-period boundaries, section metadata, explicit year selection, actual performance metrics, lot/month counts, deterministic output, changing activity headers, alternate detail levels, commission treatment and incomplete wash-sale windows.

`internal/activity/testdata/activity_layouts_anonymized.csv` preserves the stock and Forex header layouts and row widths from a real local activity statement. Account identifiers, dates, symbols and monetary values are replaced. Alternate `Trade`/`ClosedLot` levels, added Account columns and date-only trades are synthetic tests because the available statement contains only `Order` rows. Form 8949 section tests are also synthetic; no real worksheet was available to validate additional export layouts. Private statements remain ignored by Git; only the explicitly named anonymized fixture is included.

## Disclaimer

**IMPORTANT: TAX AND FINANCIAL DISCLAIMER**

This software is provided for informational purposes only and does not constitute financial, tax, or legal advice. The analysis provided by this tool is based on general wash sale rules and may not account for your specific tax situation, IRS rule changes, or special circumstances.

- The authors and contributors are not tax professionals, financial advisors, or legal experts.
- Any calculations, reports, or analyses should be verified by a qualified tax professional.
- Users assume full responsibility for any tax filing decisions made based on this tool's output.
- This tool may contain errors, bugs, or misinterpretations of tax regulations that could lead to incorrect conclusions.
- Tax laws and regulations change frequently; this tool may not reflect the most current rules.

Users should consult with qualified tax professionals before making decisions that could impact their tax obligations. By using this software, you acknowledge that you are using it at your own risk and that the authors and contributors cannot be held liable for any damages or tax liabilities that may result.

## License

[MIT](LICENSE)
