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
	out, err := captureStdout(func() error { return AnalyzeWashSalePeriods([]string{path}) })
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
	link, err := url.Parse(createGoogleCalendarLink("XYZ & A", d, "$-100.00"))
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

func TestShortCoverLossesReportedSeparatelyAndExcludedFromSafe(t *testing.T) {
	data := "Statement,Data,WhenGenerated,\"2024-02-10, 12:00:00 EST\"\n" +
		commandHeader +
		"Trades,Data,Order,Stocks,USD,SHRT,\"2024-02-01, 10:00:00\",100,5,5,-500,-1,400,-101,0,C\n" +
		"Trades,Data,Order,Stocks,USD,SHRTLD,\"2024-02-02, 10:00:00\",50,10,10,-500,0,500,0,0,C;LD\n" +
		"Trades,Data,Order,Stocks,USD,LONG,\"2024-02-03, 10:00:00\",-100,9,9,900,0,-950,-50,0,C\n" +
		"Trades,Data,Order,Stocks,USD,SAFE,\"2024-02-04, 10:00:00\",100,10,10,-1000,0,1000,0,0,O\n"

	path := writeActivity(t, data)
	periodsOut, err := captureStdout(func() error { return AnalyzeWashSalePeriods([]string{path}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"=== SHORT-COVER LOSSES REQUIRING SEPARATE REVIEW ===",
		"SHRT | 02/01/2024 | $-101.00 | 100 | $5.00",
		"SHRTLD | 02/02/2024 | $0.00 | 50 | $10.00",
		"• SAFE - No active window found in supplied trades",
	} {
		if !strings.Contains(periodsOut, want) {
			t.Fatalf("wash-sale-periods missing %q:\n%s", want, periodsOut)
		}
	}
	for _, forbidden := range []string{"• SHRT ", "• SHRTLD "} {
		if strings.Contains(periodsOut, forbidden) {
			t.Fatalf("short-cover loss listed as no active window (%q):\n%s", forbidden, periodsOut)
		}
	}

	report, err := activity.ParseActivityReportReader(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	asOf, _ := time.Parse(time.DateOnly, "2024-02-10")
	safeOut, err := captureStdout(func() error { return printSafeRepurchaseDates(report, asOf, true) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"=== SHORT-COVER LOSSES REQUIRING SEPARATE REVIEW ===",
		"SHRT | 02/01/2024 | $-101.00",
		"SHRTLD | 02/02/2024 | $0.00",
		"• SAFE - No active window found in supplied trades",
	} {
		if !strings.Contains(safeOut, want) {
			t.Fatalf("safe-repurchase missing %q:\n%s", want, safeOut)
		}
	}
	for _, forbidden := range []string{"• SHRT ", "• SHRTLD ", "Review+Repurchase+Window%3A+SHRT"} {
		if strings.Contains(safeOut, forbidden) {
			t.Fatalf("short-cover loss leaked into safe section/calendar (%q):\n%s", forbidden, safeOut)
		}
	}

	// Statement with ONLY a short-cover loss must not return early with "No active loss-sale windows found".
	shortOnly := "Statement,Data,WhenGenerated,\"2024-02-10, 12:00:00 EST\"\n" +
		commandHeader +
		"Trades,Data,Order,Stocks,USD,SHRT,\"2024-02-01, 10:00:00\",100,5,5,-500,-1,400,-101,0,C\n"
	shortOnlyReport, err := activity.ParseActivityReportReader(strings.NewReader(shortOnly))
	if err != nil {
		t.Fatal(err)
	}
	shortOnlyOut, err := captureStdout(func() error { return printSafeRepurchaseDates(shortOnlyReport, asOf, false) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shortOnlyOut, "=== SHORT-COVER LOSSES REQUIRING SEPARATE REVIEW ===") || !strings.Contains(shortOnlyOut, "SHRT | 02/01/2024 | $-101.00") {
		t.Fatalf("short-only report hid short-cover loss:\n%s", shortOnlyOut)
	}
}

func TestSafeRepurchaseTimezoneDay30Day0AndFutureTrade(t *testing.T) {
	// 1) User in Tokyo (UTC+9) during US market hours on Day 30 (2024-02-01 01:00 JST == 2024-01-31 11:00 EST).
	jst := time.FixedZone("JST", 9*3600)
	estDay30CSV := "Statement,Data,WhenGenerated,\"2024-01-31, 10:00:00 EST\"\n" +
		commandHeader +
		"Trades,Data,Order,Stocks,USD,XYZ,\"2024-01-01, 15:30:00\",-100,9,9,900,0,-1000,-100,0,C\n"
	report, err := activity.ParseActivityReportReader(strings.NewReader(estDay30CSV))
	if err != nil {
		t.Fatal(err)
	}

	tokyoDay30 := time.Date(2024, 2, 1, 1, 0, 0, 0, jst) // 2024-01-31 11:00 EST (Day 30)
	out, err := captureStdout(func() error { return printSafeRepurchaseDatesAt(report, tokyoDay30, false) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "As of: January 31, 2024 (EST)") || !strings.Contains(out, "XYZ | $-100.00 | 02/01/2024 | 1 days") {
		t.Fatalf("expected active Day 30 window in EST when invoked from JST:\n%s", out)
	}

	tokyoDay31 := time.Date(2024, 2, 1, 14, 0, 0, 0, jst) // 2024-02-01 00:00 EST (Day 31)
	out, err = captureStdout(func() error { return printSafeRepurchaseDatesAt(report, tokyoDay31, false) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No active loss-sale windows found") {
		t.Fatalf("expected Day 31 in EST to clear window:\n%s", out)
	}

	// 2) User in US Pacific (UTC-8) after midnight EST on Day 0 (2024-02-01 21:45 PST == 2024-02-02 00:45 EST).
	pst := time.FixedZone("PST", -8*3600)
	estDay0CSV := "Statement,Data,WhenGenerated,\"2024-02-02, 00:30:00 EST\"\n" +
		commandHeader +
		"Trades,Data,Order,Stocks,USD,XYZ,\"2024-02-02, 00:15:00\",-100,9,9,900,0,-1000,-100,0,C\n"
	day0Report, err := activity.ParseActivityReportReader(strings.NewReader(estDay0CSV))
	if err != nil {
		t.Fatal(err)
	}
	pacificDay0 := time.Date(2024, 2, 1, 21, 45, 0, 0, pst) // 2024-02-02 00:45 EST
	out, err = captureStdout(func() error { return printSafeRepurchaseDatesAt(day0Report, pacificDay0, false) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "As of: February 2, 2024 (EST)") || !strings.Contains(out, "XYZ | $-100.00 | 03/04/2024 | 31 days") {
		t.Fatalf("expected active Day 0 window in EST when invoked from PST:\n%s", out)
	}

	// 3) Statement without WhenGenerated containing a trade dated after comparisonDate must warn and not mark symbol safe.
	noGenCSV := commandHeader +
		"Trades,Data,Order,Stocks,USD,XYZ,\"2024-02-02, 00:15:00\",-100,9,9,900,0,-1000,-100,0,C\n" +
		"Trades,Data,Order,Stocks,USD,SAFE,\"2024-01-15, 10:00:00\",100,10,10,-1000,0,1000,0,0,O\n"
	noGenReport, err := activity.ParseActivityReportReader(strings.NewReader(noGenCSV))
	if err != nil {
		t.Fatal(err)
	}
	out, err = captureStdout(func() error { return printSafeRepurchaseDatesAt(noGenReport, pacificDay0, false) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"=== LOSS TRADES DATED AFTER AS-OF DATE ===",
		"XYZ | 02/02/2024 | Loss sale | $-100.00",
		"• SAFE - No active window found in supplied trades",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing future-trade warning %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "• XYZ ") {
		t.Fatalf("future-dated loss sale must not be listed as no active window:\n%s", out)
	}
}
