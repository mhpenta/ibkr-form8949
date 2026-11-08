package form8949

import (
	"testing"
)

func TestWashSaleInspectionRegressions(t *testing.T) {
	// Use actual column (h) values through the parser, including adjustments.
	row := func(description, acquired, sold, proceeds, basis, code, adjustment, gain string) Transaction {
		tx, err := parseTransaction([]string{"Data", description, acquired, sold, proceeds, basis, code, adjustment, gain})
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	for _, test := range []struct {
		name       string
		rows       []Transaction
		suspicious int
	}{
		{"fully disallowed loss", []Transaction{
			row("100 sh XYZ", "2023-01-01", "2024-02-01", "900", "1000", "W", "100", "0"),
			row("100 sh XYZ", "2024-02-15", "2024-03-01", "1100", "1000", "", "", "100"),
		}, 0},
		{"partial replacement with different quantity", []Transaction{
			row("100 sh XYZ", "2023-01-01", "2024-02-01", "900", "1000", "W", "50", "-50"),
			row("50 sh XYZ", "2024-02-15", "2024-03-01", "550", "500", "", "", "50"),
		}, 0},
		{"replacement disposed next year", []Transaction{
			row("100 sh XYZ", "2023-01-01", "2024-12-20", "900", "1000", "W", "100", "0"),
			row("100 sh XYZ", "2024-12-25", "2025-02-01", "1100", "1000", "", "", "100"),
		}, 0},
		{"prior purchase already disposed", []Transaction{
			row("100 sh XYZ", "2023-01-01", "2024-02-01", "900", "1000", "W", "100", "0"),
			row("100 sh XYZ", "2024-01-15", "2024-01-20", "1100", "1000", "", "", "100"),
		}, 1},
		{"unknown acquisition is not replacement evidence", []Transaction{
			row("100 sh XYZ", "2023-01-01", "2024-02-01", "900", "1000", "W", "100", "0"),
			row("100 sh XYZ", "VARIOUS", "2024-03-01", "1100", "1000", "", "", "100"),
		}, 1},
		{"actual zero loss still flagged", []Transaction{
			row("100 sh XYZ", "2023-01-01", "2024-02-01", "1000", "1000", "W", "100", "100"),
			row("100 sh XYZ", "2024-02-15", "2024-03-01", "1100", "1000", "", "", "100"),
		}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := AnalyzeTransactions(test.rows, 2024, 35, 45)
			if len(got.SuspiciousWashSales) != test.suspicious {
				t.Fatalf("suspicious = %+v, want %d", got.SuspiciousWashSales, test.suspicious)
			}
			proceeds := 0.0
			for _, tx := range test.rows {
				if tx.DateSold.Year() == 2024 {
					proceeds += tx.Proceeds
				}
			}
			if got.TotalProceeds != proceeds {
				t.Fatalf("other-year proceeds leaked into totals: %g", got.TotalProceeds)
			}
		})
	}
}
