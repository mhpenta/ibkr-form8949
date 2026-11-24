package activity

import (
	"strings"
	"testing"
	"time"
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

func TestStatementDateResolvesTimezoneOffsets(t *testing.T) {
	header := "Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code\n"
	buy := "Trades,Data,Order,Stocks,USD,XYZ,\"2024-02-15, 10:00:00\",100,10,10,-1000,0,1000,0,0,O\n"
	utcCSV := "Statement,Data,Period,\"January 1, 2024 - March 1, 2024\"\nStatement,Data,WhenGenerated,\"2024-03-01, 16:00:00 UTC\"\n" + header + buy
	estCSV := "Statement,Data,Period,\"January 1, 2024 - March 1, 2024\"\nStatement,Data,WhenGenerated,\"2024-03-01, 12:00:00 EST\"\n" + header + strings.Replace(buy, ",100,", ",150,", 1)

	utcReport, err := ParseActivityReportReader(strings.NewReader(utcCSV))
	if err != nil {
		t.Fatal(err)
	}
	estReport, err := ParseActivityReportReader(strings.NewReader(estCSV))
	if err != nil {
		t.Fatal(err)
	}

	// 12:00 EST is 17:00 UTC, which is one hour after 16:00 UTC.
	if !estReport.StatementDate.After(utcReport.StatementDate) {
		t.Fatalf("expected 12:00 EST (%s) to be after 16:00 UTC (%s)", estReport.StatementDate, utcReport.StatementDate)
	}

	merged, err := MergeReports([]*ActivityReport{utcReport, estReport}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Trades) != 1 || merged.Trades[0].Quantity != 150 {
		t.Fatalf("expected newer EST statement to win overlap: %+v", merged.Trades)
	}
	if merged.StatementLocation == nil {
		t.Fatal("expected StatementLocation to be preserved after merge")
	}

	// DST-aware conversion: a statement generated in EST converts a March UTC instant to EDT.
	marchInstant := time.Date(2024, 3, 16, 3, 30, 0, 0, time.UTC) // 2024-03-15 23:30 EDT
	converted := estReport.AsOfInStatementZone(marchInstant)
	if got := converted.Format("2006-01-02 15:04 MST"); got != "2024-03-15 23:30 EDT" {
		t.Fatalf("expected DST-aware conversion 2024-03-15 23:30 EDT, got %q", got)
	}
}

func TestGetActiveShortCoverLossesAndFutureLossTrades(t *testing.T) {
	csvData := `Statement,Data,WhenGenerated,"2024-02-10, 12:00:00 EST"
Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
Trades,Data,Order,Stocks,USD,SHRT,"2024-02-01, 10:00:00",100,15,15,-1500,0,1400,-100,0,C
Trades,Data,Order,Stocks,USD,SHRTLD,"2024-02-02, 10:00:00",100,15,15,-1500,0,1500,0,0,C;LD
Trades,Data,Order,Stocks,USD,SHRTLOT,"2024-02-03, 10:00:00",200,15,15,-3000,0,3200,200,0,C
Trades,Data,ClosedLot,Stocks,USD,SHRTLOT,2024-01-20,-100,,,,,1600,-100,,
Trades,Data,ClosedLot,Stocks,USD,SHRTLOT,2024-01-25,-100,,,,,1600,300,,
Trades,Data,Order,Stocks,USD,OLD,"2024-01-05, 10:00:00",100,15,15,-1500,0,1400,-100,0,C
Trades,Data,Order,Stocks,USD,OPEN,"2024-02-05, 10:00:00",100,15,15,-1500,0,1500,0,0,O
Trades,Data,Order,Stocks,USD,FUTURE,"2024-02-11, 10:00:00",-100,10,10,1000,0,-1100,-100,0,C
`
	report, err := ParseActivityReportReader(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}

	shortLosses := GetActiveShortCoverLosses(report)
	if len(shortLosses) != 3 {
		t.Fatalf("expected 3 active short-cover losses (SHRT, SHRTLD, SHRTLOT), got %d: %+v", len(shortLosses), shortLosses)
	}
	wantSymbols := []string{"SHRT", "SHRTLD", "SHRTLOT"}
	for i, want := range wantSymbols {
		if shortLosses[i].Symbol != want {
			t.Fatalf("short loss %d: got %s want %s", i, shortLosses[i].Symbol, want)
		}
	}
	// Buy-to-cover closed lots must not set long HasLossLot.
	for _, tr := range report.TradesBySymbol["SHRTLOT"] {
		if tr.HasLossLot || tr.IsLossSale() {
			t.Fatalf("buy-to-cover must not be marked as long loss sale: %+v", tr)
		}
	}

	future := GetFutureLossTrades(report)
	if len(future) != 1 || future[0].Symbol != "FUTURE" {
		t.Fatalf("expected 1 future loss trade for FUTURE, got %+v", future)
	}
}
