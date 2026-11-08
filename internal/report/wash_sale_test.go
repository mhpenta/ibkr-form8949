package report

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mhpenta/ibkr-form8949/internal/form8949"
)

func TestFullyDisallowedLossReport(t *testing.T) {
	analysis := form8949.TaxAnalysis{SuspiciousWashSales: []form8949.Transaction{{
		Description: "100 sh XYZ", Proceeds: 900, CostBasis: 1000,
		Adjustment: 100, GainOrLoss: 0, IsWashSale: true,
		SuspiciousReason: "No matching purchase within reasonable timeframe",
	}}}
	// A file avoids pipe-buffer limits while capturing the complete report.
	f, err := os.CreateTemp(t.TempDir(), "report-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdout
	func() {
		os.Stdout = f
		defer func() { os.Stdout = old }()
		PrintNoRepurchaseReport(analysis)
	}()
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Zero Before-Adjustment Gain/Loss: 0",
		"Before-Adjustment Loss Transactions: 1",
		"Hypothetical gain/loss if these adjustments were removed: $-100.00",
		"Replacement shares are still held",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}
