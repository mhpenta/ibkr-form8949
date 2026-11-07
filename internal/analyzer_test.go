package internal

import (
	"github.com/mhpenta/ibkr-form8949/internal/types"
	"strings"
	"testing"
	"time"
)

// TestWashSaleDetection tests the core wash sale detection logic
func TestWashSaleDetection(t *testing.T) {
	year := 2024
	extendedWindow := 35
	maxWindow := 45

	testCases := []struct {
		name               string
		transactions       []types.Transaction
		expectedSuspicious int
		description        string
	}{
		{
			name: "Basic wash sale with repurchase within 30 days",
			transactions: []types.Transaction{
				createTestTransaction("TEST SECURITY", "2024-01-01", "2024-02-01", 900, 1000, true, 100),
				createTestTransaction("TEST SECURITY", "2024-02-15", "2024-03-01", 1100, 1000, false, 0),
			},
			expectedSuspicious: 0,
			description:        "A sale at a loss followed by a repurchase within 30 days should be a valid wash sale",
		},
		{
			name: "Wash sale with repurchase just outside 30 days",
			transactions: []types.Transaction{
				createTestTransaction("TEST SECURITY", "2024-01-01", "2024-02-01", 900, 1000, true, 100),
				createTestTransaction("TEST SECURITY", "2024-03-05", "2024-04-01", 1100, 1000, false, 0),
			},
			expectedSuspicious: 1,
			description:        "A sale at a loss followed by a repurchase 33 days later should be flagged as suspicious",
		},
		{
			name: "Wash sale with no repurchase",
			transactions: []types.Transaction{
				createTestTransaction("TEST SECURITY", "2024-01-01", "2024-02-01", 900, 1000, true, 100),
				// No repurchase transaction
			},
			expectedSuspicious: 1,
			description:        "A wash sale with no matching repurchase should be flagged as suspicious",
		},
		{
			name: "Zero loss wash sale",
			transactions: []types.Transaction{
				createTestTransaction("TEST SECURITY", "2024-01-01", "2024-02-01", 1000, 1000, true, 100),
			},
			expectedSuspicious: 1,
			description:        "A wash sale with zero loss should be flagged as suspicious",
		},
		{
			name: "Multiple wash sales with one repurchase",
			transactions: []types.Transaction{
				createTestTransaction("TEST SECURITY", "2024-01-01", "2024-02-01", 900, 1000, true, 100),
				createTestTransaction("TEST SECURITY", "2024-01-05", "2024-02-05", 850, 1000, true, 150),
				createTestTransaction("TEST SECURITY", "2024-02-15", "2024-03-01", 1100, 1000, false, 0),
			},
			expectedSuspicious: 0,
			description:        "Multiple wash sales sharing one repurchase within 30 days should be valid",
		},
		{
			name: "Different securities should not trigger wash sales",
			transactions: []types.Transaction{
				createTestTransaction("TEST SECURITY A", "2024-01-01", "2024-02-01", 900, 1000, true, 100),
				createTestTransaction("TEST SECURITY B", "2024-02-15", "2024-03-01", 1100, 1000, false, 0),
			},
			expectedSuspicious: 1,
			description:        "Different securities should not count as repurchases for each other's wash sales",
		},
		{
			name: "Large loss with wash sale adjustment",
			transactions: []types.Transaction{
				createTestTransaction("100 sh APPLE INC", "2023-06-01", "2024-06-14", 15000, 20000, true, 1500),
				// No repurchase
			},
			expectedSuspicious: 1,
			description:        "Large loss with wash sale adjustment but no nearby repurchase",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			analysis := AnalyzeTransactions(tc.transactions, year, extendedWindow, maxWindow)

			if len(analysis.SuspiciousWashSales) != tc.expectedSuspicious {
				t.Errorf("Expected %d suspicious wash sales, got %d - %s",
					tc.expectedSuspicious, len(analysis.SuspiciousWashSales), tc.description)

				// Print details of what was found if expectations aren't met
				if len(analysis.SuspiciousWashSales) > 0 {
					t.Logf("Suspicious wash sales found:")
					for i, tx := range analysis.SuspiciousWashSales {
						t.Logf("  %d. %s sold on %s - Reason: %s",
							i+1, tx.Description, tx.DateSold.Format("2006-01-02"), tx.SuspiciousReason)
					}
				}
			}
		})
	}
}

// TestDaysBetweenCalculation tests the calculation of days between transactions
func TestDaysBetweenCalculation(t *testing.T) {
	testCases := []struct {
		name         string
		sold         string
		acquired     string
		expectedDays int
	}{
		{
			name:         "Same day",
			sold:         "2024-01-01",
			acquired:     "2024-01-01",
			expectedDays: 0,
		},
		{
			name:         "One day after",
			sold:         "2024-01-01",
			acquired:     "2024-01-02",
			expectedDays: 1,
		},
		{
			name:         "30 days after (edge of window)",
			sold:         "2024-01-01",
			acquired:     "2024-01-31",
			expectedDays: 30,
		},
		{
			name:         "31 days after (just outside window)",
			sold:         "2024-01-01",
			acquired:     "2024-02-01",
			expectedDays: 31,
		},
		{
			name:         "One day before",
			sold:         "2024-01-02",
			acquired:     "2024-01-01",
			expectedDays: -1,
		},
		{
			name:         "30 days before (edge of window)",
			sold:         "2024-01-31",
			acquired:     "2024-01-01",
			expectedDays: -30,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sold, _ := time.Parse("2006-01-02", tc.sold)
			acquired, _ := time.Parse("2006-01-02", tc.acquired)

			// Calculate days between using the same method as in analyzeTransactions
			daysBetween := int(acquired.Sub(sold).Hours() / 24)

			if daysBetween != tc.expectedDays {
				t.Errorf("Expected %d days between %s and %s, got %d",
					tc.expectedDays, tc.sold, tc.acquired, daysBetween)
			}
		})
	}
}

// TestYearFiltering tests that only transactions in the specified year are analyzed
func TestYearFiltering(t *testing.T) {
	targetYear := 2024
	extendedWindow := 35
	maxWindow := 45

	transactions := []types.Transaction{
		createTestTransaction("TEST SECURITY", "2023-01-01", "2023-12-31", 900, 1000, true, 100), // Previous year
		createTestTransaction("TEST SECURITY", "2024-01-01", "2024-02-01", 900, 1000, true, 100), // Target year
		createTestTransaction("TEST SECURITY", "2025-01-01", "2025-02-01", 900, 1000, true, 100), // Next year
	}

	analysis := AnalyzeTransactions(transactions, targetYear, extendedWindow, maxWindow)

	// Only the 2024 transaction should be included in the analysis
	expectedTotal := 1
	if len(analysis.WashSaleTransactions) != expectedTotal {
		t.Errorf("Expected %d total wash sales in year %d, got %d",
			expectedTotal, targetYear, len(analysis.WashSaleTransactions))
	}

	// The 2024 transaction should be suspicious (no repurchase)
	if len(analysis.SuspiciousWashSales) != 1 {
		t.Errorf("Expected the 2024 transaction to be flagged as suspicious")
	}
}

// TestLargeLossWashSaleWithoutRepurchase checks a large wash-sale loss with no
// repurchase in any window.
func TestLargeLossWashSaleWithoutRepurchase(t *testing.T) {
	year := 2024
	extendedWindow := 35
	maxWindow := 60 // Use a wider window

	largeLossTx := createTestTransaction("100 sh APPLE INC", "2023-06-01", "2024-06-14", 15000, 20000, true, 1500)

	analysis := AnalyzeTransactions([]types.Transaction{largeLossTx}, year, extendedWindow, maxWindow)

	// The transaction should be marked as suspicious
	if len(analysis.SuspiciousWashSales) != 1 {
		t.Fatalf("Expected 1 suspicious wash sale, got %d", len(analysis.SuspiciousWashSales))
	}

	tx := analysis.SuspiciousWashSales[0]

	// Verify the correct reason is assigned
	expectedReason := "No matching purchase within reasonable timeframe"
	if tx.SuspiciousReason != expectedReason {
		t.Errorf("Expected reason '%s', got '%s'", expectedReason, tx.SuspiciousReason)
	}

	// Verify the financial details
	expectedAdjustment := 1500.0
	if !floatEquals(tx.Adjustment, expectedAdjustment, 0.01) {
		t.Errorf("Expected adjustment $%.2f, got $%.2f", expectedAdjustment, tx.Adjustment)
	}

	expectedLoss := -5000.0
	if !floatEquals(tx.GainOrLoss, expectedLoss, 0.01) {
		t.Errorf("Expected loss $%.2f, got $%.2f", expectedLoss, tx.GainOrLoss)
	}

	// Test the tax impact calculation
	taxImpact := tx.Adjustment - tx.GainOrLoss
	expectedImpact := 6500.0
	if !floatEquals(taxImpact, expectedImpact, 0.01) {
		t.Errorf("Expected tax impact $%.2f, got $%.2f", expectedImpact, taxImpact)
	}
}

// TestPositionAnalysis tests the position analysis functionality
func TestPositionAnalysis(t *testing.T) {
	year := 2024
	extendedWindow := 35
	maxWindow := 45

	// Create test transactions with a variety of winning and losing trades
	transactions := []types.Transaction{
		// AAPL - 2 winning trades, 1 losing trade
		createTestTransaction("AAPL", "2024-01-01", "2024-02-01", 1100, 1000, false, 0), // +100 gain
		createTestTransaction("AAPL", "2024-03-01", "2024-04-01", 1200, 1000, false, 0), // +200 gain
		createTestTransaction("AAPL", "2024-05-01", "2024-06-01", 900, 1000, false, 0),  // -100 loss

		// GOOG - 1 winning trade, 2 losing trades
		createTestTransaction("GOOG", "2024-01-01", "2024-02-01", 2100, 2000, false, 0),  // +100 gain
		createTestTransaction("GOOG", "2024-03-01", "2024-04-01", 1800, 2000, true, 200), // -200 loss (wash sale)
		createTestTransaction("GOOG", "2024-05-01", "2024-06-01", 1900, 2000, false, 0),  // -100 loss

		// MSFT - 3 winning trades
		createTestTransaction("MSFT", "2024-01-01", "2024-02-01", 1100, 1000, false, 0), // +100 gain
		createTestTransaction("MSFT", "2024-03-01", "2024-04-01", 1200, 1000, false, 0), // +200 gain
		createTestTransaction("MSFT", "2024-05-01", "2024-06-01", 1300, 1000, false, 0), // +300 gain
	}

	analysis := AnalyzeTransactions(transactions, year, extendedWindow, maxWindow)

	// Verify we have position analysis data
	if len(analysis.PositionAnalysis) != 3 {
		t.Fatalf("Expected 3 positions, got %d", len(analysis.PositionAnalysis))
	}

	// Check that positions are sorted by gain/loss (most profitable first)
	if len(analysis.PositionAnalysis) >= 2 &&
		analysis.PositionAnalysis[0].TotalGainOrLoss < analysis.PositionAnalysis[1].TotalGainOrLoss {
		t.Errorf("Positions are not sorted by gain/loss correctly")
	}

	// Test MSFT position data - should be the most profitable
	msftFound := false
	for _, pos := range analysis.PositionAnalysis {
		if pos.SecurityName == "MSFT" {
			msftFound = true

			// MSFT had 3 transactions, all winning
			if pos.TransactionCount != 3 {
				t.Errorf("Expected 3 MSFT transactions, got %d", pos.TransactionCount)
			}

			if pos.WinningTrades != 3 {
				t.Errorf("Expected 3 winning MSFT trades, got %d", pos.WinningTrades)
			}

			if pos.LosingTrades != 0 {
				t.Errorf("Expected 0 losing MSFT trades, got %d", pos.LosingTrades)
			}

			// Should have 100% hit rate
			expectedHitRate := 100.0
			if !floatEquals(pos.HitRate, expectedHitRate, 0.01) {
				t.Errorf("Expected MSFT hit rate %.2f%%, got %.2f%%", expectedHitRate, pos.HitRate)
			}

			// Total gain should be 600 (100+200+300)
			expectedGain := 600.0
			if !floatEquals(pos.TotalGainOrLoss, expectedGain, 0.01) {
				t.Errorf("Expected MSFT total gain $%.2f, got $%.2f", expectedGain, pos.TotalGainOrLoss)
			}

			// Average win amount should be 200 (600/3)
			expectedAvgWin := 200.0
			if !floatEquals(pos.AvgWinAmount, expectedAvgWin, 0.01) {
				t.Errorf("Expected MSFT avg win $%.2f, got $%.2f", expectedAvgWin, pos.AvgWinAmount)
			}

			// Profit factor should be very high since no losses
			if pos.ProfitFactor < 999.0 {
				t.Errorf("Expected MSFT profit factor to be very high, got %.2f", pos.ProfitFactor)
			}
		}
	}

	if !msftFound {
		t.Error("MSFT position not found in position analysis")
	}

	// Test AAPL position data
	aaplFound := false
	for _, pos := range analysis.PositionAnalysis {
		if pos.SecurityName == "AAPL" {
			aaplFound = true

			// AAPL had 3 transactions: 2 winning, 1 losing
			if pos.TransactionCount != 3 {
				t.Errorf("Expected 3 AAPL transactions, got %d", pos.TransactionCount)
			}

			if pos.WinningTrades != 2 {
				t.Errorf("Expected 2 winning AAPL trades, got %d", pos.WinningTrades)
			}

			if pos.LosingTrades != 1 {
				t.Errorf("Expected 1 losing AAPL trade, got %d", pos.LosingTrades)
			}

			// Hit rate should be 66.67%
			expectedHitRate := 66.67
			if !floatEquals(pos.HitRate, expectedHitRate, 0.1) {
				t.Errorf("Expected AAPL hit rate %.2f%%, got %.2f%%", expectedHitRate, pos.HitRate)
			}

			// Total gain should be 200 (100+200-100)
			expectedGain := 200.0
			if !floatEquals(pos.TotalGainOrLoss, expectedGain, 0.01) {
				t.Errorf("Expected AAPL total gain $%.2f, got $%.2f", expectedGain, pos.TotalGainOrLoss)
			}

			// Average win amount should be 150 ((100+200)/2)
			expectedAvgWin := 150.0
			if !floatEquals(pos.AvgWinAmount, expectedAvgWin, 0.01) {
				t.Errorf("Expected AAPL avg win $%.2f, got $%.2f", expectedAvgWin, pos.AvgWinAmount)
			}

			// Average loss amount should be 100
			expectedAvgLoss := 100.0
			if !floatEquals(pos.AvgLossAmount, expectedAvgLoss, 0.01) {
				t.Errorf("Expected AAPL avg loss $%.2f, got $%.2f", expectedAvgLoss, pos.AvgLossAmount)
			}

			// Profit factor should be 3.0 (300/100)
			expectedProfitFactor := 3.0
			if !floatEquals(pos.ProfitFactor, expectedProfitFactor, 0.01) {
				t.Errorf("Expected AAPL profit factor %.2f, got %.2f", expectedProfitFactor, pos.ProfitFactor)
			}
		}
	}

	if !aaplFound {
		t.Error("AAPL position not found in position analysis")
	}
}

// Helper function for approximate float comparison
func floatEquals(a, b, epsilon float64) bool {
	return (a-b) < epsilon && (b-a) < epsilon
}

// Helper function to create test transactions
func createTestTransaction(description string, dateAcquired, dateSold string, proceeds, costBasis float64, isWashSale bool, adjustment float64) types.Transaction {
	da, _ := time.Parse("2006-01-02", dateAcquired)
	ds, _ := time.Parse("2006-01-02", dateSold)

	var codes string
	if isWashSale {
		codes = "W"
	}

	return types.Transaction{
		Description:   description,
		DateAcquired:  da,
		DateSold:      ds,
		Proceeds:      proceeds,
		CostBasis:     costBasis,
		GainOrLoss:    proceeds - costBasis,
		IsWashSale:    isWashSale,
		Adjustment:    adjustment,
		Codes:         codes,
		HasVariousLot: strings.Contains(dateAcquired, "VARIOUS"),
		Quarter:       int((ds.Month()-1)/3 + 1),
	}
}

func TestPositionPerformanceConsolidation(t *testing.T) {
	transactions := []types.Transaction{
		createTestTransaction("100 sh XYZ", "2024-01-01", "2024-02-01", 2000, 1000, false, 0),
		createTestTransaction("XYZ 200 sh", "2024-01-01", "2024-02-02", 100, 1000, false, 0),
		createTestTransaction("XYZ 20SEP24 100 C", "2024-01-01", "2024-02-02", 200, 100, false, 0),
		createTestTransaction("XYZ 20SEP24 110 C", "2024-01-01", "2024-02-02", 200, 100, false, 0),
	}
	a := AnalyzeTransactions(transactions, 2024, 35, 45)
	if len(a.PositionAnalysis) != 3 {
		t.Fatalf("expected stock and two distinct contracts: %+v", a.PositionAnalysis)
	}
	for _, p := range a.PositionAnalysis {
		if p.SecurityName == "XYZ" {
			if p.TransactionCount != 2 || p.TotalGainOrLoss != 100 || p.AvgWinAmount != 1000 || p.AvgLossAmount != 900 {
				t.Fatalf("incorrect stock totals: %+v", p)
			}
			return
		}
	}
	t.Fatal("stock position missing")
}
