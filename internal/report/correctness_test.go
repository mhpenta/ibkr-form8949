package report

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal"
	"github.com/mhpenta/ibkr-form8949/internal/types"
)

func captureReport(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	previous := os.Stdout
	func() { os.Stdout = f; defer func() { os.Stdout = previous }(); fn() }()
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestConsolidatedMetricsUseActualGrossAmounts(t *testing.T) {
	a := types.TaxAnalysis{TotalGainOrLoss: 100, PositionAnalysis: []types.PositionSummary{
		{SecurityName: "100 sh XYZ", TransactionCount: 1, WinningTrades: 1, TotalGainOrLoss: 1000, AvgWinAmount: 1000},
		{SecurityName: "XYZ 200 sh", TransactionCount: 1, LosingTrades: 1, TotalGainOrLoss: -900, AvgLossAmount: 900},
	}}
	out := captureReport(t, func() { PrintPositionPerformanceReport(a) })
	for _, want := range []string{"Profit Factor:            1.11", "XYZ | 2 | 50.0% | $100.00 | $1000.00 | $900.00 | $50.00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestReportIdentityPreservesOptionContracts(t *testing.T) {
	if ExtractBaseName("100 sh xyz") != "XYZ" || ExtractBaseName("XYZ 20SEP24 100 Call") == ExtractBaseName("XYZ 20SEP24 110 Call") || ExtractBaseName("XYZ 20SEP24 100 Call") == "XYZ" {
		t.Fatal("inconsistent or collapsed identities")
	}
}

func TestProfitFactorWithoutLosses(t *testing.T) {
	a := internal.AnalyzeTransactions([]types.Transaction{{Description: "XYZ", DateSold: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Quarter: 1, GainOrLoss: 250}}, 2024, 35, 45)
	out := captureReport(t, func() { PrintPositionPerformanceReport(a); PrintPositionAnalysis(a) })
	if !strings.Contains(out, "Profit Factor:            +Inf") || strings.Contains(out, "999.99") {
		t.Fatalf("invented finite profit factor:\n%s", out)
	}
}

func TestLotCountsMonthsAndUnknownTerms(t *testing.T) {
	date := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	rows := []types.Transaction{
		{Description: "100 sh XYZ", DateSold: date("2024-01-01"), Quarter: 1, GainOrLoss: 100},
		{Description: "200 sh XYZ", DateSold: date("2024-01-02"), Quarter: 1, GainOrLoss: 200},
		{Description: "XYZ", DateSold: date("2024-02-01"), Quarter: 1, GainOrLoss: 300, IsLongTerm: true, HasVariousLot: true},
		{Description: "XYZ", DateSold: date("2024-03-01"), Quarter: 1, GainOrLoss: 400, TermUnknown: true, HasVariousLot: true},
	}
	a := internal.AnalyzeTransactions(rows, 2024, 35, 45)
	out := captureReport(t, func() { PrintLotsReport(a); PrintQuarterlyAnalysis(a); PrintStandardAnalysis(a) })
	for _, want := range []string{"Short-term | 2 | $300.00", "Long-term | 1 | $300.00", "Unknown term | 1 | $400.00", "VARIOUS acquisitions (included above) | 2 | $700.00", "01 | 2 | $300.00", "02 | 1 | $300.00", "03 | 1 | $400.00", "does not calculate estimated tax payments"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Multiply by your") || strings.Contains(out, "Approx. Monthly") {
		t.Fatal("retained misleading estimates")
	}
}

func TestEmptyAndBreakEvenReports(t *testing.T) {
	a := types.TaxAnalysis{PositionAnalysis: []types.PositionSummary{{SecurityName: "XYZ", TransactionCount: 1}}}
	out := captureReport(t, func() {
		PrintPositionPerformanceReport(a)
		PrintPositionAnalysis(a)
		PrintPositionPerformanceReport(types.TaxAnalysis{})
	})
	if strings.Contains(out, "NaN") || strings.Contains(out, "Losing Trades:            0 (100.00%)") {
		t.Fatalf("incorrect rates:\n%s", out)
	}
}

func TestDeterministicAnalysisReports(t *testing.T) {
	rows := []types.Transaction{{Description: "BBB", DateSold: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Quarter: 1, CostBasis: 100, IsWashSale: true, Adjustment: 100}, {Description: "AAA", DateSold: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Quarter: 1, CostBasis: 100, IsWashSale: true, Adjustment: 100}}
	previous := ""
	for i := 0; i < 12; i++ {
		a := internal.AnalyzeTransactions(rows, 2024, 35, 45)
		out := captureReport(t, func() { PrintAll(a); PrintNoRepurchaseReport(a) })
		if i > 0 && out != previous {
			t.Fatal("map iteration changed output")
		}
		previous = out
	}
}
