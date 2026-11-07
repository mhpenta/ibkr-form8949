package activity

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestActivityWashSaleCodes(t *testing.T) {
	for _, test := range []struct {
		code string
		wash bool
	}{
		{"LD", true}, {"C;LD", true}, {"C; LD;P", true},
		{"W", false}, {"C;W", false}, {"XLD", false}, {"C", false}, {"", false},
	} {
		csv := fmt.Sprintf(`Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
Trades,Data,Order,Stocks,USD,XYZ,"2024-01-15, 10:00:00",-100,10,10,1000,-1,1100,0,0,%s
`, test.code)
		r, err := ParseActivityReportReader(strings.NewReader(csv))
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Trades[0].IsWashSale; got != test.wash {
			t.Errorf("code %q: wash = %v, want %v", test.code, got, test.wash)
		}
	}
}

func stockTrade(date string, quantity, pl float64, wash bool) Trade {
	d, err := time.Parse("2006-01-02 15:04", date)
	if err != nil {
		panic(err)
	}
	return Trade{Symbol: "XYZ", DateTime: d, Quantity: quantity, RealizedPL: pl,
		IsBuy: quantity > 0, IsSale: quantity < 0, IsWashSale: wash}
}

func TestReplacementLotAllocation(t *testing.T) {
	for _, test := range []struct {
		name    string
		trades  []Trade
		matched []float64
		reviews int
	}{
		{"original acquisition is consumed", []Trade{
			stockTrade("2024-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-02 09:00", -100, 0, true),
		}, []float64{0}, 1},
		{"day 30 inclusive", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 09:00", -100, 0, true),
			stockTrade("2024-01-31 23:59", 100, 0, false),
		}, []float64{100}, 0},
		{"day 31 outside", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 16:00", -100, 0, true),
			stockTrade("2024-02-01 09:00", 100, 0, false),
		}, []float64{0}, 1},
		{"prior day 30 held shares", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-31 23:59", -100, 0, true),
		}, []float64{100}, 0},
		{"prior day 31 outside", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 16:00", 100, 0, false),
			stockTrade("2024-02-01 09:00", -100, 0, true),
		}, []float64{0}, 1},
		{"only unsold portion of original purchase qualifies", []Trade{
			stockTrade("2024-01-01 09:00", 150, 0, false),
			stockTrade("2024-01-02 09:00", -100, 0, true),
		}, []float64{50}, 1},
		{"multiple lots add to full replacement", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 09:00", -100, 0, true),
			stockTrade("2024-01-02 09:00", 40, 0, false),
			stockTrade("2024-01-03 09:00", 60, 0, false),
		}, []float64{100}, 0},
		{"one replacement cannot cover two losses", []Trade{
			stockTrade("2023-01-01 09:00", 200, 0, false),
			stockTrade("2024-01-01 09:00", -100, 0, true),
			stockTrade("2024-01-02 09:00", -100, 0, true),
			stockTrade("2024-01-03 09:00", 100, 0, false),
		}, []float64{100, 0}, 1},
		{"disposed lot cannot be reused", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-02 09:00", -100, 50, false),
			stockTrade("2024-01-03 09:00", -100, 0, true),
		}, []float64{0}, 1},
		{"same day purchase after sale", []Trade{
			stockTrade("2023-01-01 09:00", 100, 0, false),
			stockTrade("2024-01-01 09:00", -100, 0, true),
			stockTrade("2024-01-01 10:00", 100, 0, false),
		}, []float64{100}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := AnalyzeSymbol(test.trades)
			if len(a.WashSaleChecks) != len(test.matched) {
				t.Fatalf("checks = %+v", a.WashSaleChecks)
			}
			for i, want := range test.matched {
				if got := a.WashSaleChecks[i].MatchedQuantity; got != want {
					t.Errorf("check %d matched %g, want %g", i, got, want)
				}
			}
			if len(a.SuspiciousTrades) != test.reviews {
				t.Errorf("review count = %d, want %d: %+v", len(a.SuspiciousTrades), test.reviews, a.SuspiciousTrades)
			}
		})
	}
}

func TestMissingAndNonFIFOLotsRequireReview(t *testing.T) {
	for _, code := range []string{"C", "C;LI", "C;SL"} {
		trades := []Trade{
			stockTrade("2024-01-01 09:00", -100, 0, true),
			stockTrade("2024-01-02 09:00", 100, 0, false),
		}
		trades[0].Code = code
		if code != "C" {
			trades = append([]Trade{stockTrade("2023-01-01 09:00", 100, 0, false)}, trades...)
		}
		a := AnalyzeSymbol(trades)
		if len(a.SuspiciousTrades) != 1 || !strings.Contains(a.SuspiciousTrades[0].Reason, "closed-lot details") {
			t.Fatalf("code %q: %+v", code, a.SuspiciousTrades)
		}
	}
}

func TestCoverPurchaseIsNotLongReplacement(t *testing.T) {
	trades := []Trade{
		stockTrade("2023-01-01 09:00", 100, 0, false),
		stockTrade("2024-01-01 09:00", -100, 0, true),
		stockTrade("2024-01-02 09:00", 100, 0, false),
	}
	trades[2].Code = "C"
	if got := AnalyzeSymbol(trades).WashSaleChecks[0].MatchedQuantity; got != 0 {
		t.Fatalf("cover matched %g shares", got)
	}
}

func TestShortOpeningDoesNotConsumeReplacements(t *testing.T) {
	trades := []Trade{
		stockTrade("2023-01-01 09:00", 100, 0, false),
		stockTrade("2024-01-01 09:00", -100, 0, true),
		stockTrade("2024-01-02 09:00", -100, 0, true),
		stockTrade("2024-01-03 09:00", 100, 0, false),
	}
	trades[1].Code = "O;LD"
	a := AnalyzeSymbol(trades)
	if a.WashSaleChecks[0].MatchedQuantity != 0 || a.WashSaleChecks[1].MatchedQuantity != 100 {
		t.Fatalf("short opening consumed long replacements: %+v", a.WashSaleChecks)
	}
}

func TestActivePeriodCalendarBoundaries(t *testing.T) {
	sale := stockTrade("2024-03-01 16:00", -100, 0, true)
	for _, test := range []struct {
		date   string
		active bool
	}{
		{"2024-02-29T12:00:00-07:00", false},
		{"2024-03-01T16:00:00-07:00", true},
		{"2024-03-31T23:59:00-06:00", true},
		{"2024-04-01T00:00:00-06:00", false},
		{"2024-04-02T12:00:00-06:00", false},
	} {
		date, _ := time.Parse(time.RFC3339, test.date)
		periods := getSymbolWashSalePeriods("XYZ", []Trade{sale}, date)
		if (len(periods) == 1) != test.active {
			t.Errorf("as of %s: %+v", test.date, periods)
		}
		if len(periods) == 1 && periods[0].PeriodEndDate.Format("2006-01-02 15:04") != "2024-04-01 00:00" {
			t.Errorf("unexpected first date: %s", periods[0].PeriodEndDate)
		}
	}
}
