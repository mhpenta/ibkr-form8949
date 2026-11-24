package activity

import (
	"math"
	"testing"
	"time"
)

func statTrade(day string, quantity, price, commission float64, code string) Trade {
	date, _ := time.Parse(time.DateOnly, day)
	return Trade{
		AssetCategory: "Stocks", Currency: "USD", Symbol: "XYZ", DateTime: date,
		Quantity: quantity, TradePrice: price, Proceeds: -quantity * price, CommFee: -commission,
		Basis: quantity*price + commission, Code: code, IsBuy: quantity > 0, IsSale: quantity < 0,
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAnalyzePositionAveragesAndTimeline(t *testing.T) {
	sale := statTrade("2024-02-05", -150, 9, 1, "C")
	sale.Basis = -1601 // FIFO basis of 100 @ 10 and 50 @ 12, with commissions
	trades := []Trade{
		statTrade("2024-04-10", 50, 11.25, 1, "O"), // out of order on purpose
		statTrade("2024-01-03", 100, 10, 1, "O"),
		statTrade("2024-01-20", 100, 12, 1, "O"),
		sale,
		statTrade("2024-02-20", 50, 9.5, 1, "O"),
	}
	s := AnalyzePosition(trades, 0)

	if s.Buys.Trades != 4 || s.Buys.Shares != 300 || !near(s.Buys.AveragePrice(), 3237.5/300) || !near(s.Buys.AverageNetPrice(), 3241.5/300) {
		t.Fatalf("buy averages: %+v", s.Buys)
	}
	if s.Buys.LowPrice != 9.5 || s.Buys.MedianPrice != 10 || s.Buys.HighPrice != 12 || s.Buys.MedianTrade != 75 {
		t.Fatalf("buy distribution: %+v", s.Buys)
	}
	if s.Sells.Shares != 150 || !near(s.Sells.AverageNetPrice(), 1349.0/150) || !near(s.AverageSoldCost(), 1601.0/150) {
		t.Fatalf("sell averages: %+v, sold cost %v", s.Sells, s.AverageSoldCost())
	}
	wantPosition := []float64{100, 200, 50, 100, 150}
	wantOpen := []float64{10, 11, 11, 10.25, (100*10.25 + 50*11.25) / 150}
	for i, step := range s.Steps {
		if step.Position != wantPosition[i] || !step.OpenPriceKnown || !near(step.OpenPrice, wantOpen[i]) {
			t.Fatalf("step %d: %+v", i, step)
		}
	}
	if len(s.Months) != 3 || s.Months[1].Sells.Shares != 150 || s.Months[1].EndPosition != 100 || s.Months[2].Month.Month() != time.April {
		t.Fatalf("months: %+v", s.Months)
	}
	if s.HasShortActivity() {
		t.Fatal("no short activity expected")
	}
}

func TestOpenPriceUnknownUntilOpeningSharesAreGone(t *testing.T) {
	s := AnalyzePosition([]Trade{
		statTrade("2024-01-02", 50, 20, 0, ""),
		statTrade("2024-01-03", -150, 21, 0, ""),
		statTrade("2024-01-04", 30, 22, 0, ""),
	}, 100)
	if s.Steps[0].Position != 150 || s.Steps[0].OpenPriceKnown {
		t.Fatalf("opening shares have no known price: %+v", s.Steps[0])
	}
	if s.Steps[1].Position != 0 || !s.Steps[1].OpenPriceKnown || s.Steps[1].OpenPrice != 0 {
		t.Fatalf("flat position makes the average known: %+v", s.Steps[1])
	}
	if final := s.Final(100); final.Position != 30 || !final.OpenPriceKnown || final.OpenPrice != 22 {
		t.Fatalf("new position after flat: %+v", final)
	}
}

func TestShortActivityAndCrossingZero(t *testing.T) {
	s := AnalyzePosition([]Trade{
		statTrade("2024-01-02", 100, 10, 0, "O"),
		statTrade("2024-01-03", -150, 12, 0, "C;O"), // closes the long, opens 50 short
		statTrade("2024-01-04", -50, 13, 0, "O"),
		statTrade("2024-01-05", 100, 11, 0, "C"),
	}, 0)
	if got := s.Steps[1]; got.Position != -50 || got.OpenPrice != 12 {
		t.Fatalf("crossing zero opens the remainder at the trade price: %+v", got)
	}
	if got := s.Steps[2]; got.Position != -100 || !near(got.OpenPrice, 12.5) {
		t.Fatalf("adding to a short averages: %+v", got)
	}
	if !s.HasShortActivity() || s.Actions[ActionShortSale].Shares != 50 || s.Actions[ActionCover].Shares != 100 || s.Actions[ActionSell].Shares != 150 {
		t.Fatalf("actions: %+v", s.Actions)
	}
	if s.SoldShares != 150 {
		t.Fatalf("only long sales count toward sold cost: %v", s.SoldShares)
	}
}

func TestPriceBandsUseRoundEdges(t *testing.T) {
	bands := priceBands([]Trade{
		statTrade("2024-01-02", 10, 0.1, 0, ""),
		statTrade("2024-01-03", -5, 0.3, 0, ""),
		statTrade("2024-01-04", 7, 1.0, 0, ""),
	})
	if len(bands) != 10 || bands[0].Low != 0.1 || bands[0].High != 0.2 || bands[len(bands)-1].Low != 1.0 {
		t.Fatalf("bands: %+v", bands)
	}
	if bands[0].Bought != 10 || bands[2].Sold != 5 || bands[2].Low != 0.3 || bands[len(bands)-1].Bought != 7 {
		t.Fatalf("a price on a band edge belongs to the band starting there: %+v", bands)
	}
	if one := priceBands([]Trade{statTrade("2024-01-02", 10, 5, 0, "")}); len(one) != 1 || one[0].Bought != 10 {
		t.Fatalf("single price: %+v", one)
	}
}
