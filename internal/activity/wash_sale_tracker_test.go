package activity

import (
	"strings"
	"testing"
)

func TestParseActivityReportUsesStatementDateForActivePeriods(t *testing.T) {
	csvData := `Statement,Data,Period,2024
Statement,Data,WhenGenerated,"2024-01-20, 12:00:00 MST"
Account Information,Data,Account,U1234567
Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
Trades,Data,Order,Stocks,USD,XYZ,"2024-01-15, 10:00:00",-100,10,10,1000,-1,1100,-100,0,
`

	report, err := ParseActivityReportReader(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseActivityReportReader failed: %v", err)
	}

	if report.StatementDate.IsZero() {
		t.Fatal("expected statement date to be parsed")
	}

	if !report.Today.Equal(report.StatementDate) {
		t.Fatalf("expected report.Today to use statement date, got %s want %s",
			report.Today, report.StatementDate)
	}

	periods := GetActiveWashSalePeriods(report)
	if len(periods) != 1 {
		t.Fatalf("expected one active period as of statement date, got %d", len(periods))
	}
	if periods[0].Symbol != "XYZ" {
		t.Fatalf("expected active period for XYZ, got %s", periods[0].Symbol)
	}
}

func TestGetSafeSymbolsExcludesActiveSymbols(t *testing.T) {
	allSymbols := []string{"AAA", "BBB", "CCC"}
	activePeriods := []WashSalePeriod{
		{Symbol: "BBB"},
	}

	safe := getSafeSymbols(allSymbols, activePeriods)
	want := []string{"AAA", "CCC"}

	if len(safe) != len(want) {
		t.Fatalf("expected %d safe symbols, got %d: %v", len(want), len(safe), safe)
	}
	for i := range want {
		if safe[i] != want[i] {
			t.Fatalf("safe symbol %d: got %s want %s", i, safe[i], want[i])
		}
	}
}
