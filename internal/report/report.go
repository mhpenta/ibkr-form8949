// Package report provides various reports for Form 8949 tax analysis
package report

import (
	"fmt"

	"github.com/mhpenta/ibkr-form8949/internal/types"
)

// PrintAll prints all available reports
func PrintAll(analysis types.TaxAnalysis) {
	PrintStandardAnalysis(analysis)
	fmt.Println()
	PrintQuarterlyAnalysis(analysis)
	fmt.Println()
	PrintPositionAnalysis(analysis)
	fmt.Println()
	PrintLotsReport(analysis)
	fmt.Println()
	PrintPositionPerformanceReport(analysis)
}

// HandleReportType executes the appropriate report based on the specified type
func HandleReportType(reportType string, analysis types.TaxAnalysis) {
	switch reportType {
	case "standard":
		PrintStandardAnalysis(analysis)
	case "quarterly":
		PrintQuarterlyAnalysis(analysis)
	case "position":
		PrintPositionAnalysis(analysis)
	case "lots":
		PrintLotsReport(analysis)
	case "position-performance":
		PrintPositionPerformanceReport(analysis)
	case "all":
		PrintAll(analysis)
	default:
		// Default to standard analysis
		PrintStandardAnalysis(analysis)
	}
}

// HandleFocusReport executes the appropriate focus report
func HandleFocusReport(focusType string, analysis types.TaxAnalysis) bool {
	switch focusType {
	case "no-repurchase":
		PrintNoRepurchaseReport(analysis)
		return true
	default:
		return false
	}
}
