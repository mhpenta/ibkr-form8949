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

func TestMissingAndUnsupportedLotsRequireReview(t *testing.T) {
	for _, code := range []string{"C", "C;SL", "C;ML", "C;MSL"} {
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

func lotTrades(code string, trades ...Trade) []Trade {
	for i := range trades {
		if trades[i].IsSale {
			trades[i].Code = code
		}
	}
	return trades
}

func TestLotMethodChoosesSoldShares(t *testing.T) {
	// Old lot 2023-12-01 at $20, new lot 2024-01-20 at $10, loss sale of 100
	// on 2024-01-25. FIFO sells the old lot, leaving the new lot (5 days
	// before) as replacement; LIFO sells the new lot, leaving the old one, 55
	// days before and outside the window. Highest cost also sells the $20 lot.
	trades := func() []Trade {
		old := stockTrade("2023-12-01 09:00", 100, 0, false)
		old.Basis = 2000
		recent := stockTrade("2024-01-20 09:00", 100, 0, false)
		recent.Basis = 1000
		return []Trade{old, recent, stockTrade("2024-01-25 09:00", -100, -50, false)}
	}
	for _, test := range []struct {
		code    string
		method  LotMethod
		matched float64
	}{
		{"C", LotFIFO, 100},
		{"C;LI", LotLIFO, 0},
		{"C;HC", LotHighestCost, 100},
	} {
		checks := inspectReplacementLots(lotTrades(test.code, trades()...), nil)
		check := checks[2]
		if check.LotMethod != test.method || check.MatchedQuantity != test.matched || check.Reason == "" && test.matched == 0 {
			t.Errorf("code %q: %+v", test.code, check)
		}
		if strings.Contains(check.Reason, "closed-lot details") {
			t.Errorf("code %q needlessly requires review: %s", test.code, check.Reason)
		}
	}
}

func TestLIFOSaleKeepsOlderReplacement(t *testing.T) {
	// LIFO sells today's lot from the 2024-01-05 purchase; the 2024-01-02
	// purchase stays and is 8 days before the loss: a full replacement.
	checks := inspectReplacementLots(lotTrades("C;LI",
		stockTrade("2024-01-02 09:00", 100, 0, false),
		stockTrade("2024-01-05 09:00", 100, 0, false),
		stockTrade("2024-01-10 09:00", -100, -40, false),
	), nil)
	if got := checks[2].MatchedQuantity; got != 100 {
		t.Fatalf("matched %g: %+v", got, checks[2])
	}
}

func TestOpeningInventoryOrdersSales(t *testing.T) {
	// 100 shares held before the statement plus a 2024-01-20 purchase. FIFO
	// sells the opening shares and keeps the purchase as a replacement; without
	// the opening quantity it would wrongly sell the purchase instead.
	trades := []Trade{
		stockTrade("2024-01-20 09:00", 100, 0, false),
		stockTrade("2024-01-25 09:00", -100, -50, false),
	}
	key := tradeInventoryKey(trades[0])
	if got := inspectReplacementLots(trades, map[string]float64{key: 100})[1].MatchedQuantity; got != 100 {
		t.Fatalf("FIFO with opening shares matched %g", got)
	}
	if got := inspectReplacementLots(trades, nil)[1].MatchedQuantity; got != 0 {
		t.Fatalf("FIFO without opening shares matched %g", got)
	}
	// LIFO sells the purchase first; opening shares are undated, never a replacement.
	lifo := lotTrades("C;LI", trades...)
	if check := inspectReplacementLots(lifo, map[string]float64{key: 100})[1]; check.MatchedQuantity != 0 {
		t.Fatalf("LIFO matched %+v", check)
	}
	// Highest cost cannot rank opening shares of unknown cost.
	hc := lotTrades("C;HC", trades...)
	hc[0].Basis = 1000
	if check := inspectReplacementLots(hc, map[string]float64{key: 100})[1]; !strings.Contains(check.Reason, "closed-lot details") {
		t.Fatalf("highest cost with opening shares: %+v", check)
	}
}

func TestClosedLotsOverrideElection(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	trades := func(lots ...ClosedLot) []Trade {
		trades := lotTrades("C;SL",
			stockTrade("2023-12-01 09:00", 100, 0, false),
			stockTrade("2024-01-20 09:00", 100, 0, false),
			stockTrade("2024-01-25 09:00", -100, -50, false),
		)
		trades[2].ClosedLots = lots
		return trades
	}
	for _, test := range []struct {
		name    string
		lots    []ClosedLot
		matched float64
		review  bool
	}{
		{"sold the old lot", []ClosedLot{{Acquired: day("2023-12-01"), Quantity: 100}}, 100, false},
		{"sold the recent lot", []ClosedLot{{Acquired: day("2024-01-20"), Quantity: 100}}, 0, false},
		{"split across lots", []ClosedLot{{Acquired: day("2023-12-01"), Quantity: 60}, {Acquired: day("2024-01-20"), Quantity: 40}}, 60, false},
		{"lots short of the sale", []ClosedLot{{Acquired: day("2023-12-01"), Quantity: 60}}, 100, true},
		{"predates the statement", []ClosedLot{{Acquired: day("2023-06-01"), Quantity: 100}}, 100, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			check := inspectReplacementLots(trades(test.lots...), nil)[2]
			review := strings.Contains(check.Reason, "closed-lot details")
			if check.MatchedQuantity != test.matched || review != test.review {
				t.Fatalf("%+v", check)
			}
			if !test.review && check.LotMethod != LotClosedLots {
				t.Fatalf("method %q", check.LotMethod)
			}
		})
	}
}

func TestClosedLotsBeyondPurchaseDayRequireReview(t *testing.T) {
	// Both sales name the single 100-share purchase of 2024-01-20; the second
	// cannot be satisfied from that day, so inventory is no longer reliable.
	day, _ := time.Parse(time.DateOnly, "2024-01-20")
	trades := []Trade{
		stockTrade("2024-01-20 09:00", 100, 0, false),
		stockTrade("2024-01-22 09:00", -100, -10, false),
		stockTrade("2024-01-23 09:00", -100, -10, false),
		stockTrade("2024-01-24 09:00", 100, 0, false),
	}
	trades[1].ClosedLots = []ClosedLot{{Acquired: day, Quantity: 100}}
	trades[2].ClosedLots = []ClosedLot{{Acquired: day, Quantity: 100}}
	checks := inspectReplacementLots(trades, nil)
	if strings.Contains(checks[1].Reason, "closed-lot details") || !strings.Contains(checks[2].Reason, "closed-lot details") {
		t.Fatalf("first %+v\nsecond %+v", checks[1], checks[2])
	}
}
