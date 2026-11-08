package main

import (
	"testing"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
)

func TestSecurityReportTitleUsesStatementPeriodYear(t *testing.T) {
	records := [][]string{
		{"Statement", "Data", "Period", "January 1, 2025 - December 31, 2025"},
	}

	got := securityReportTitle(records)
	want := "2025 STOCK TRADE-ROW PERFORMANCE REPORT"
	if got != want {
		t.Fatalf("securityReportTitle() = %q, want %q", got, want)
	}
}

func TestSecurityReportTitleUsesRangeWhenPeriodSpansYears(t *testing.T) {
	records := [][]string{
		{"Statement", "Data", "Period", "December 15, 2024 - January 15, 2025"},
	}

	got := securityReportTitle(records)
	want := "2024-2025 STOCK TRADE-ROW PERFORMANCE REPORT"
	if got != want {
		t.Fatalf("securityReportTitle() = %q, want %q", got, want)
	}
}

func TestSecurityReportTitleFallsBackWithoutPeriod(t *testing.T) {
	got := securityReportTitle(nil)
	want := "STOCK TRADE-ROW PERFORMANCE REPORT"
	if got != want {
		t.Fatalf("securityReportTitle() = %q, want %q", got, want)
	}
}

func TestFormatCurrencySetSortsCurrencies(t *testing.T) {
	currencies := map[string]struct{}{
		"USD": {},
		"AUD": {},
	}

	got := formatCurrencySet(currencies)
	want := "AUD, USD"
	if got != want {
		t.Fatalf("formatCurrencySet() = %q, want %q", got, want)
	}
}

func TestMixedCurrencySuffix(t *testing.T) {
	tests := []struct {
		name       string
		currencies map[string]struct{}
		want       string
	}{
		{
			name:       "empty",
			currencies: nil,
			want:       "",
		},
		{
			name: "single currency",
			currencies: map[string]struct{}{
				"USD": {},
			},
			want: " USD",
		},
		{
			name: "multiple currencies",
			currencies: map[string]struct{}{
				"USD": {},
				"AUD": {},
			},
			want: " (mixed currencies: AUD, USD)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mixedCurrencySuffix(tt.currencies)
			if got != tt.want {
				t.Fatalf("mixedCurrencySuffix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAggregateMarkToMarketPerformanceAggregatesDetailsOnly(t *testing.T) {
	rows := []activity.MarkToMarketPerformance{
		{
			AssetCategory:   "Stocks",
			Symbol:          "GOOGL",
			CurrentQuantity: 100,
			PositionPL:      1200,
			TotalPL:         1200,
		},
		{
			AssetCategory: "Stocks",
			Symbol:        "GOOGL",
			PositionPL:    350.5,
			TransactionPL: -40.25,
			CommissionPL:  -1.25,
			OtherPL:       10.5,
			TotalPL:       319.5,
		},
		{
			AssetCategory: "Total",
			TotalPL:       1519.5,
		},
		{
			AssetCategory: "Broker Interest Paid and Received",
			TotalPL:       -100,
		},
		{
			AssetCategory: "Equity and Index Options",
			Symbol:        "MSFT 19DEC25 500 C",
			PositionPL:    -800.5,
			TransactionPL: -50.25,
			CommissionPL:  12.5,
			TotalPL:       -838.25,
		},
	}

	performances := aggregateMarkToMarketPerformance(rows)
	if len(performances) != 2 {
		t.Fatalf("aggregateMarkToMarketPerformance length = %d, want 2", len(performances))
	}

	bySymbol := make(map[string]MarkToMarketSecurityPerformance)
	for _, performance := range performances {
		bySymbol[performance.Symbol] = performance
	}

	googl, ok := bySymbol["GOOGL"]
	if !ok {
		t.Fatal("missing GOOGL aggregate")
	}
	if googl.CurrentQuantity != 100 {
		t.Fatalf("GOOGL CurrentQuantity = %v, want 100", googl.CurrentQuantity)
	}
	if googl.PositionPL != 1550.5 {
		t.Fatalf("GOOGL PositionPL = %v, want 1550.5", googl.PositionPL)
	}
	if googl.TotalPL != 1519.5 {
		t.Fatalf("GOOGL TotalPL = %v, want 1519.5", googl.TotalPL)
	}

	msft, ok := bySymbol["MSFT 19DEC25 500 C"]
	if !ok {
		t.Fatal("missing MSFT option aggregate")
	}
	if msft.TotalPL != -838.25 {
		t.Fatalf("MSFT option TotalPL = %v, want -838.25", msft.TotalPL)
	}
}

func TestMarkToMarketReportTitleUsesStatementPeriod(t *testing.T) {
	records := [][]string{
		{"Statement", "Data", "Period", "January 1, 2025 - December 31, 2025"},
	}

	got := markToMarketReportTitle(records)
	want := "2025 BASE-CURRENCY MARK-TO-MARKET PERFORMANCE REPORT"
	if got != want {
		t.Fatalf("markToMarketReportTitle() = %q, want %q", got, want)
	}
}
