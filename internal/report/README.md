# Report Package

This package provides various report formats for tax analysis of Interactive Brokers Form 8949 data.

## Structure

The report package is organized into multiple files, each responsible for a specific type of report:

- `report.go`: Main entry point and report routing
- `common.go`: Common utility functions used across all reports
- `standard.go`: Standard analysis report with wash sale identification
- `quarterly.go`: Calendar-quarter realized gain/loss analysis
- `position.go`: Position-based performance analysis
- `lots.go`: Detailed lot-based analysis
- `position_performance.go`: Consolidated position performance analysis
- `no_repurchase.go`: Focused report on wash sales with no matching repurchase

## Report Types

### Standard Report
The standard report provides a summary of Form 8949 data, including proceeds, cost basis, adjustments, and suspicious wash sales.

### Quarterly Analysis
The quarterly analysis shows gains/losses broken down by calendar quarter, including unknown holding periods. Calendar quarters are not the IRS estimated-tax payment periods; this report does not calculate estimated payments.

### Position Analysis
The position analysis provides detailed metrics for each trading position, including win rates, profit factors, and average win/loss amounts.

### Lots Analysis
The lots analysis focuses on individual trading lots, showing statistics by security and holding period.

### Position Performance Report
The position performance report consolidates trades by security after removing explicit share-count text, while preserving option contract identity. Averages and profit factors use actual gross gains and losses.

### No Repurchase Report
The no repurchase report focuses specifically on wash sales with no matching repurchase activity, helping identify potentially incorrect wash sale adjustments.

## Usage

The main entry points for the package are:

```go
// Handle a standard report type
report.HandleReportType(reportType, analysis)

// Handle a focused report
report.HandleFocusReport(focusType, analysis)

// Print all reports
report.PrintAll(analysis)
```
