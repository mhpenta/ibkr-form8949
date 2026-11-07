package activity

import (
	"strings"
	"testing"
	"time"
)

func TestParseActivityReportParsesMarkToMarketPerformance(t *testing.T) {
	csvData := `Statement,Data,Period,"January 1, 2025 - December 31, 2025"
Account Information,Data,Account,U123
Account Information,Data,Base Currency,USD
Change in NAV,Header,Field Name,Field Value
Change in NAV,Data,Starting Value,1000
Change in NAV,Data,Mark-to-Market,600
Change in NAV,Data,Deposits & Withdrawals,-100
Change in NAV,Data,Position Transfers,-50
Change in NAV,Data,Dividends,25
Change in NAV,Data,Interest,-10
Change in NAV,Data,Commissions,-5
Change in NAV,Data,Ending Value,1500
Mark-to-Market Performance Summary,Header,Asset Category,Symbol,Prior Quantity,Current Quantity,Prior Price,Current Price,Mark-to-Market P/L Position,Mark-to-Market P/L Transaction,Mark-to-Market P/L Commissions,Mark-to-Market P/L Other,Mark-to-Market P/L Total,Code
Mark-to-Market Performance Summary,Data,Stocks,AAPL,100,0,150.0000,--,-1200,-300.25,-1.5,0,-1501.75,
Mark-to-Market Performance Summary,Data,Equity and Index Options,MSFT 19DEC25 500 C,0,0,--,--,-800.5,-50.25,12.5,0,-838.25,
Mark-to-Market Performance Summary,Data,Total (All Assets),,,,,,-2000.5,-350.5,11,0,-2340,
Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
`

	report, err := ParseActivityReportReader(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseActivityReportReader failed: %v", err)
	}

	if report.BaseCurrency != "USD" {
		t.Fatalf("BaseCurrency = %q, want USD", report.BaseCurrency)
	}

	if !report.ChangeInNAV.HasData() {
		t.Fatal("ChangeInNAV.HasData() = false, want true")
	}
	if report.ChangeInNAV.StartingValue != 1000 {
		t.Fatalf("StartingValue = %v, want 1000", report.ChangeInNAV.StartingValue)
	}
	if report.ChangeInNAV.EndingValue != 1500 {
		t.Fatalf("EndingValue = %v, want 1500", report.ChangeInNAV.EndingValue)
	}
	if report.ChangeInNAV.NetNAVChange() != 500 {
		t.Fatalf("NetNAVChange() = %v, want 500", report.ChangeInNAV.NetNAVChange())
	}
	if report.ChangeInNAV.ExternalFlows() != -150 {
		t.Fatalf("ExternalFlows() = %v, want -150", report.ChangeInNAV.ExternalFlows())
	}
	if report.ChangeInNAV.GainBeforeExternalFlows() != 650 {
		t.Fatalf("GainBeforeExternalFlows() = %v, want 650", report.ChangeInNAV.GainBeforeExternalFlows())
	}

	if len(report.MarkToMarketPerformance) != 3 {
		t.Fatalf("MarkToMarketPerformance length = %d, want 3", len(report.MarkToMarketPerformance))
	}

	apple := report.MarkToMarketPerformance[0]
	if apple.AssetCategory != "Stocks" {
		t.Fatalf("AssetCategory = %q, want Stocks", apple.AssetCategory)
	}
	if apple.Symbol != "AAPL" {
		t.Fatalf("Symbol = %q, want AAPL", apple.Symbol)
	}
	if apple.PriorQuantity != 100 {
		t.Fatalf("PriorQuantity = %v, want 100", apple.PriorQuantity)
	}
	if apple.CurrentQuantity != 0 {
		t.Fatalf("CurrentQuantity = %v, want 0", apple.CurrentQuantity)
	}
	if apple.PriorPrice != 150 {
		t.Fatalf("PriorPrice = %v, want 150", apple.PriorPrice)
	}
	if apple.CurrentPrice != 0 {
		t.Fatalf("CurrentPrice = %v, want 0 for --", apple.CurrentPrice)
	}
	if apple.PositionPL != -1200 {
		t.Fatalf("PositionPL = %v, want -1200", apple.PositionPL)
	}
	if apple.TransactionPL != -300.25 {
		t.Fatalf("TransactionPL = %v, want -300.25", apple.TransactionPL)
	}
	if apple.CommissionPL != -1.5 {
		t.Fatalf("CommissionPL = %v, want -1.5", apple.CommissionPL)
	}
	if apple.OtherPL != 0 {
		t.Fatalf("OtherPL = %v, want 0", apple.OtherPL)
	}
	if apple.TotalPL != -1501.75 {
		t.Fatalf("TotalPL = %v, want -1501.75", apple.TotalPL)
	}

	if got := len(report.MarkToMarketPerformanceBySymbol["AAPL"]); got != 1 {
		t.Fatalf("MarkToMarketPerformanceBySymbol[AAPL] length = %d, want 1", got)
	}
}

func TestParseActivityReportRequiresBothQuantitiesForFlatCheckpoint(t *testing.T) {
	const header = "Mark-to-Market Performance Summary,Header,Asset Category,Symbol,Prior Quantity,Current Quantity,Mark-to-Market P/L Total\n"
	const snapshot = "Mark-to-Market Performance Summary,Data,Stocks,XYZ,"
	const trades = `Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,Proceeds,Comm/Fee,Basis,Realized P/L,Code
Trades,Data,Order,Stocks,USD,XYZ,2024-01-01,100,10,-1000,-1,1001,0,O
Trades,Data,Order,Stocks,USD,XYZ,2024-02-01,-100,9,900,-1,-1001,-102,C
`
	for _, tt := range []struct {
		quantities string
		wantFlat   bool
	}{
		{"0,0", true},
		{"0,--", false},
		{",0", false},
	} {
		t.Run(tt.quantities, func(t *testing.T) {
			report, err := ParseActivityReportReader(strings.NewReader(header + snapshot + tt.quantities + ",0\n" + trades))
			if err != nil {
				t.Fatal(err)
			}
			if got := report.Trades[1].PositionKnownFlat; got != tt.wantFlat {
				t.Fatalf("PositionKnownFlat = %t, want %t", got, tt.wantFlat)
			}
		})
	}
}

func TestParseActivityReportAcceptsDateOnlyTrade(t *testing.T) {
	csvData := `Statement,Data,Period,"January 1, 2026 - July 31, 2026"
Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
Trades,Data,Order,Stocks,KRW,005930.KS,2026-07-08,-10,70000,70500,700000,-1750,-650000,48250,-5000,C;LI
`

	report, err := ParseActivityReportReader(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("ParseActivityReportReader failed: %v", err)
	}
	if len(report.Trades) != 1 {
		t.Fatalf("Trades length = %d, want 1", len(report.Trades))
	}

	want := time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC)
	if got := report.Trades[0].DateTime; !got.Equal(want) {
		t.Fatalf("DateTime = %v, want %v", got, want)
	}
}
