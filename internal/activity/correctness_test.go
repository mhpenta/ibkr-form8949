package activity

import (
	"os"
	"strings"
	"testing"
	"time"
)

const activityHeader = "Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code\n"
const activityBuy = "Trades,Data,Order,Stocks,USD,XYZ,\"2024-01-01, 10:00:00\",100,10,10,-1000,-1,1001,0,0,O\n"
const activitySale = "Trades,Data,Order,Stocks,USD,XYZ,\"2024-02-01, 10:00:00\",-100,11,11,1100,-1,-1001,98,0,C\n"

func TestAnonymizedActualActivityLayouts(t *testing.T) {
	// Header layouts, blank columns, and row widths are derived from the local
	// statement. Account, dates, securities and all amounts are replacements.
	f, err := os.Open("testdata/activity_layouts_anonymized.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	report, err := ParseActivityReportReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Trades) != 2 {
		t.Fatalf("trades=%d", len(report.Trades))
	}
	for _, trade := range report.Trades {
		if trade.CommFee != -1 || trade.Code != "O" {
			t.Fatalf("wrong header mapping: %+v", trade)
		}
	}
	if report.Trades[1].AssetCategory != "Forex" || report.Trades[1].Proceeds != -110 {
		t.Fatalf("wrong Forex row: %+v", report.Trades[1])
	}
}

// Alternate detail levels, reordered/account columns, and date-only examples
// below are synthetic; the supplied real statement has only Order rows.
func TestTradeDetailLevels(t *testing.T) {
	executions := strings.ReplaceAll(activityBuy+activitySale, ",Order,", ",Trade,")
	closedLot := strings.Replace(activitySale, ",Order,", ",ClosedLot,", 1)
	for name, data := range map[string]string{
		"orders":                         activityBuy + activitySale,
		"executions":                     executions,
		"orders and executions and lots": activityBuy + activitySale + executions + closedLot,
	} {
		t.Run(name, func(t *testing.T) {
			r, err := ParseActivityReportReader(strings.NewReader(activityHeader + data))
			if err != nil {
				t.Fatal(err)
			}
			pl := 0.0
			for _, trade := range r.Trades {
				pl += trade.RealizedPL
			}
			if len(r.Trades) != 2 || pl != 98 {
				t.Fatalf("duplicate or missing transactions: %+v", r.Trades)
			}
		})
	}
	_, err := ParseActivityReportReader(strings.NewReader(activityHeader + activityBuy + activitySale + strings.Replace(executions, ",98,", ",97,", 1)))
	if err == nil || !strings.Contains(err.Error(), "totals disagree") {
		t.Fatalf("inconsistent detail accepted: %v", err)
	}
}

func TestActivityHeaderMappingAndDates(t *testing.T) {
	header := "Trades,Header,Account,DataDiscriminator,Asset Category,Currency,Symbol,Quantity,Date/Time,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code\n"
	row := "Trades,Data,SAMPLE,Order,Stocks,USD,xyz,-100,2024-02-01,9,9,900,-1,-1001,0,0,C;LD\n"
	r, err := ParseActivityReportReader(strings.NewReader(activityHeader + activityBuy + header + row))
	if err != nil {
		t.Fatal(err)
	}
	trade := r.Trades[1]
	if trade.Symbol != "XYZ" || trade.Account != "SAMPLE" || !trade.DateOnly || !trade.IsWashSale || trade.Proceeds != 900 {
		t.Fatalf("bad mapping: %+v", trade)
	}
	// A missing optional Code field must remain empty, not read column zero.
	r, err = ParseActivityReportReader(strings.NewReader(strings.Replace(activityHeader, ",Code", "", 1) + strings.TrimSuffix(activityBuy, ",O\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Trades[0].Code != "" || len(r.Warnings) == 0 {
		t.Fatalf("missing-code handling: %+v", r)
	}
}

func TestActivityRejectsMalformedData(t *testing.T) {
	for name, data := range map[string]string{
		"truncated row":           activityHeader + "Trades,Data,Order,Stocks,USD,XYZ\n",
		"truncated code":          activityHeader + strings.TrimSuffix(activityBuy, ",O\n") + "\n",
		"missing required header": strings.Replace(activityHeader, "Realized P/L", "Other", 1) + activityBuy,
		"bad proceeds":            activityHeader + strings.Replace(activityBuy, "-1000", "bad", 1),
		"nonfinite":               activityHeader + strings.Replace(activityBuy, "-1000", "Inf", 1),
		"unknown detail":          activityHeader + strings.Replace(activityBuy, ",Order,", ",Mystery,", 1),
		"no header":               activityBuy,
		"bad NAV":                 "Change in NAV,Data,Starting Value,bad\n" + activityHeader + activityBuy,
	} {
		t.Run(name, func(t *testing.T) {
			r, err := ParseActivityReportReader(strings.NewReader(data))
			if err == nil || r != nil || !strings.Contains(err.Error(), "line") {
				t.Fatalf("accepted invalid data: %+v, %v", r, err)
			}
		})
	}
}

func TestAmbiguousOrderingCannotEstablishFlatInventory(t *testing.T) {
	trades := []Trade{
		stockTrade("2024-01-01 00:00", -100, 0, true),
		stockTrade("2024-01-01 10:00", 100, 0, false),
		stockTrade("2024-02-01 10:00", -100, 0, true),
	}
	for i := range trades {
		trades[i].AssetCategory = "Stocks"
		trades[i].Currency = "USD"
	}
	trades[0].DateOnly = true
	r := &ActivityReport{Trades: trades, MarkToMarketPerformanceBySymbol: map[string][]MarkToMarketPerformance{"XYZ": {{AssetCategory: "Stocks", Symbol: "XYZ", PriorQuantity: 100, CurrentQuantity: 0}}}}
	markKnownFlatPositions(r)
	if r.Trades[0].PositionKnownFlat || !r.Trades[2].PositionKnownFlat {
		t.Fatal("flat checkpoint must require unambiguous ordering")
	}
}

func TestCoverageUsesStatementPeriodNotGeneration(t *testing.T) {
	r, err := ParseActivityReportReader(strings.NewReader("Statement,Data,Period,\"January 1, 2024 - February 1, 2024\"\nStatement,Data,WhenGenerated,\"2024-04-01, 12:00:00 MST\"\n" + activityHeader + activityBuy + activitySale))
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	if !r.CoversDates(day("2024-01-01"), day("2024-02-01")) || r.CoversDates(day("2024-02-01"), day("2024-03-02")) || r.CoversDates(day("2024-04-01"), day("2024-04-01")) {
		t.Fatal("incorrect coverage")
	}
}

func TestReplacementInventoryRecoveryRequiresBrokerEvidence(t *testing.T) {
	trades := []Trade{
		stockTrade("2024-01-01 10:00", -100, 0, true),
		stockTrade("2024-03-01 10:00", 100, 0, false),
		stockTrade("2024-04-01 10:00", -100, 0, true),
		stockTrade("2024-04-02 10:00", 100, 0, false),
	}
	for i := range trades {
		trades[i].AssetCategory = "Stocks"
		trades[i].Currency = "USD"
	}
	a := AnalyzeSymbol(trades)
	if a.WashSaleChecks[1].Reason == "" {
		t.Fatal("recovered without broker evidence")
	}
	r := &ActivityReport{Trades: trades, MarkToMarketPerformanceBySymbol: map[string][]MarkToMarketPerformance{"XYZ": {{AssetCategory: "Stocks", Symbol: "XYZ", PriorQuantity: 100, CurrentQuantity: 100}}}}
	markKnownFlatPositions(r)
	if !r.Trades[0].PositionKnownFlat {
		t.Fatal("reconciled flat position not recognized")
	}
	a = AnalyzeSymbol(r.Trades)
	if a.WashSaleChecks[0].Reason == "" || a.WashSaleChecks[1].Reason != "" || a.WashSaleChecks[1].MatchedQuantity != 100 {
		t.Fatalf("incorrect recovery: %+v", a.WashSaleChecks)
	}
}

func TestReplacementAccountsAndSameDayUncertainty(t *testing.T) {
	trades := []Trade{stockTrade("2023-01-01 10:00", 100, 0, false), stockTrade("2024-02-01 10:00", -100, 0, true), stockTrade("2024-02-02 10:00", 100, 0, false)}
	trades[0].Account = "A"
	trades[1].Account = "A"
	trades[2].Account = "B"
	if got := AnalyzeSymbol(trades).WashSaleChecks[0].MatchedQuantity; got != 0 {
		t.Fatalf("matched across accounts: %g", got)
	}
	trades[2].Account = "A"
	trades[2].DateTime = trades[1].DateTime
	trades[2].DateOnly = true
	if check := AnalyzeSymbol(trades).WashSaleChecks[0]; check.Reason == "" {
		t.Fatal("ambiguous same-day order accepted")
	}
}
