// Package form8949 parses Interactive Brokers Form 8949 CSV exports and
// analyzes them for wash sales.
//
// Transaction is one Form 8949 row. AnalyzeTransactions produces a TaxAnalysis
// and flags wash-sale adjustments that have no supporting repurchase in the
// supplied rows; PositionSummary and QuarterlyTaxData back the per-security
// and quarterly reports.
package form8949
