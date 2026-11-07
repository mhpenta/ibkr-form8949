# Types Package

This package defines the core data structures used throughout the ibkr-form8949 application.

## Data Structures

### Transaction
Represents a single stock transaction from Form 8949, with details like dates, proceeds, cost basis, and wash sale information.

### TaxAnalysis
Holds the aggregated tax analysis results, including total proceeds, cost basis, adjustments, and suspicious wash sales.

### QuarterlyTaxData
Contains tax analysis for a specific quarter, used for quarterly reporting.

### PositionSummary
Holds position-specific analysis data, including metrics like hit rate, average win/loss amounts, and profit factor.

## Purpose

The types package was created to avoid circular dependencies in the codebase while still allowing type sharing across packages. By centralizing these data structures, we ensure consistency throughout the application while maintaining a clean architecture.

## Usage

Import this package when you need to work with the core data structures:

```go
import "github.com/mhpenta/ibkr-form8949/internal/types"

// Create a new Transaction
tx := types.Transaction{
    Description: "100 sh AAPL",
    // ...
}

// Access TaxAnalysis fields
analysis := types.TaxAnalysis{
    // ...
}
fmt.Printf("Total Gain/Loss: $%.2f\n", analysis.TotalGainOrLoss)
```