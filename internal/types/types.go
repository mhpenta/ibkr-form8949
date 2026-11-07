package types

import "time"

// Transaction represents a single stock transaction from Form 8949
type Transaction struct {
	Description      string
	DateAcquired     time.Time
	DateSold         time.Time
	Proceeds         float64
	CostBasis        float64
	Codes            string
	Adjustment       float64
	GainOrLoss       float64
	IsWashSale       bool
	HasVariousLot    bool   // "VARIOUS" aggregated entry
	IsLongTerm       bool   // Worksheet term, or more than one calendar year
	TermUnknown      bool   // VARIOUS acquisition without section metadata
	TermSource       string // worksheet or dates
	Box              string
	Quarter          int    // Calendar quarter (1-4)
	CustomData       string // Additional data (e.g., closest repurchase days)
	SuspiciousReason string // Why this wash sale is suspicious
}

// UnadjustedGainOrLoss is the result before Form 8949 column (g) adjustments.
// GainOrLoss is the reported, adjusted column (h) amount.
func (tx Transaction) UnadjustedGainOrLoss() float64 {
	return tx.Proceeds - tx.CostBasis
}

// TaxAnalysis holds the aggregated tax analysis results
type TaxAnalysis struct {
	Transactions         []Transaction // Dispositions in the selected reporting year
	TotalProceeds        float64
	TotalCostBasis       float64
	TotalAdjustment      float64
	TotalGainOrLoss      float64
	TotalWashSaleAdjust  float64
	WashSaleTransactions []Transaction
	SuspiciousWashSales  []Transaction

	// Short-term vs Long-term gains
	ShortTermGainOrLoss   float64
	LongTermGainOrLoss    float64
	UnknownTermGainOrLoss float64

	// Quarterly analysis
	QuarterlyAnalysis [4]QuarterlyTaxData

	// Position analysis
	PositionAnalysis []PositionSummary

	// Configuration
	ExtendedWindow int
	MaxWindow      int
}

// QuarterlyTaxData holds tax analysis for a specific quarter
type QuarterlyTaxData struct {
	Quarter             int
	TotalGainOrLoss     float64
	ShortTermGainLoss   float64
	LongTermGainLoss    float64
	UnknownTermGainLoss float64
	TotalProceeds       float64
	TotalCostBasis      float64
	TransactionCount    int
}

// PositionSummary holds position-specific analysis data
type PositionSummary struct {
	SecurityName        string
	TransactionCount    int
	TotalProceeds       float64
	TotalCostBasis      float64
	TotalGainOrLoss     float64
	ShortTermGainLoss   float64
	LongTermGainLoss    float64
	UnknownTermGainLoss float64
	WinningTrades       int
	LosingTrades        int
	HitRate             float64 // Win percentage
	AvgWinAmount        float64 // Average profit per winning trade
	AvgLossAmount       float64 // Average loss per losing trade
	ProfitFactor        float64 // Gross profit / gross loss
}
