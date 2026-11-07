package main

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
)

func TestWashSalePeriodsOrderByLatestWindowPerSymbol(t *testing.T) {
	data := "Statement,Data,WhenGenerated,\"2026-11-06, 12:00:00 MST\"\n" + commandHeader + `Trades,Data,Order,Stocks,USD,AMZN,2026-10-16,-200,180.5,180.5,36100,0,-36300,-200,0,C
Trades,Data,Order,Stocks,USD,NVDA,2026-10-14,-80,120.25,120.25,9620,0,-10000,-380,0,C
Trades,Data,Order,Stocks,USD,AAA,2026-10-14,-50,10,10,500,0,-550,-50,0,C
Trades,Data,Order,Stocks,USD,AMZN,2026-10-22,-20,181,181,3620,0,-3650,-30,0,C
Trades,Data,Order,Stocks,USD,NVDA,2026-10-07,-100,125.5,125.5,12550,0,-13000,-450,0,C
`
	path := writeActivity(t, data)
	out, err := captureStdout(func() error { return AnalyzeWashSalePeriods(path) })
	if err != nil {
		t.Fatal(err)
	}
	want := `AAA | 10/14/2026 | 11/14/2026 | 8 days | $-50.00 | 50 | $10.00
NVDA | 10/07/2026 | 11/07/2026 | 1 days | $-450.00 | 100 | $125.50
NVDA | 10/14/2026 | 11/14/2026 | 8 days | $-380.00 | 80 | $120.25
AMZN | 10/16/2026 | 11/16/2026 | 10 days | $-200.00 | 200 | $180.50
AMZN | 10/22/2026 | 11/22/2026 | 16 days | $-30.00 | 20 | $181.00
`
	if !strings.Contains(out, want) {
		t.Fatalf("expected rows in order:\n%s\ngot:\n%s", want, out)
	}
}

func TestRepurchaseReportDay30And31(t *testing.T) {
	sale := time.Date(2024, 1, 1, 16, 0, 0, 0, time.UTC)
	report := &activity.ActivityReport{TradesBySymbol: map[string][]activity.Trade{
		"XYZ": {{Symbol: "XYZ", DateTime: sale, Quantity: -100, RealizedPL: -100, IsSale: true}},
	}}
	for _, test := range []struct{ date, expected string }{
		{"2024-01-31T23:59:00-07:00", "XYZ | $-100.00 | 02/01/2024 | 1 days"},
		{"2024-02-01T00:00:00-07:00", "No active loss-sale windows found"},
	} {
		d, _ := time.Parse(time.RFC3339, test.date)
		out, err := captureStdout(func() error { return printSafeRepurchaseDates(report, d, true) })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, test.expected) || strings.Contains(out, "SAFE NOW") {
			t.Fatalf("as of %s:\n%s", test.date, out)
		}
		if !strings.Contains(out, "Only supplied trades are checked") {
			t.Fatal("missing scope on report")
		}
		if !report.Today.IsZero() {
			t.Fatal("report's as-of date was mutated")
		}
		if strings.Contains(test.date, "01-31") && !strings.Contains(out, "dates=20240201%2F20240202") {
			t.Fatalf("wrong calendar date: %s", out)
		}
	}
}

func TestCalendarLinkUsesExclusiveNextDay(t *testing.T) {
	d := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	link, err := url.Parse(createGoogleCalendarLink("XYZ & A", d, -100))
	if err != nil {
		t.Fatal(err)
	}
	if got := link.Query().Get("dates"); got != "20241231/20250101" {
		t.Fatalf("dates = %q", got)
	}
	if !strings.Contains(link.Query().Get("text"), "XYZ & A") {
		t.Fatal("symbol lost in URL encoding")
	}
}
