package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
)

const commandHeader = "Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code\n"
const commandBuy = "Trades,Data,Order,Stocks,USD,XYZ,\"2024-01-01, 10:00:00\",100,10,10,-1000,-1,1001,0,0,O\n"
const commandSale = "Trades,Data,Order,Stocks,USD,XYZ,2024-02-01,-100,11,11,1100,-1,-1001,98,0,C\n"

func writeActivity(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "activity.csv")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAllAndPositionShareTradeParser(t *testing.T) {
	data := commandHeader + commandBuy + commandSale + strings.Replace(commandSale, ",Order,", ",ClosedLot,", 1)
	path := writeActivity(t, data)
	all, err := captureStdout(func() error { return AnalyzeAllSecurities(path) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Total Trades: 2", "Total Trade-Row Realized P/L: 98.00 USD", "Total Trade-Row Commission: -2.00 USD"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "96.00") || strings.Contains(all, "Net Trade-Row P/L (after commissions)") {
		t.Fatal("commissions deducted twice")
	}
	position, err := captureStdout(func() error { return AnalyzeSymbolPosition(path, "xyz") })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Total Sell Transactions: 1", "Total Reported Realized P/L: $98.00", "Net Period Trade Qty: 0", "Current position unknown"} {
		if !strings.Contains(position, want) {
			t.Fatalf("missing %q:\n%s", want, position)
		}
	}
	if strings.Contains(position, "Total Economic P/L:") || strings.Contains(position, "fully liquidated") {
		t.Fatal("unsupported position or economic claim")
	}
}

func TestPositionDoesNotClearUnobservedWindow(t *testing.T) {
	data := "Statement,Data,Period,\"January 1, 2024 - February 1, 2024\"\n" + commandHeader + commandBuy + strings.Replace(commandSale, ",98,", ",-102,", 1)
	path := writeActivity(t, data)
	out, err := captureStdout(func() error { return AnalyzeSymbolPosition(path, "XYZ") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Statement does not cover the full window") || strings.Contains(out, "window clear") || strings.Contains(out, "tax return should") {
		t.Fatalf("unsupported conclusion:\n%s", out)
	}
}

func TestTransactionPLAndFullCalendarWindow(t *testing.T) {
	// Both boundary dates are included, even at midnight or late on day +30.
	data := commandHeader + commandSale
	for _, date := range []string{"2024-01-02, 00:00:00", "2024-02-01, 11:00:00", "2024-03-02, 23:59:00", "2024-03-03, 00:00:00"} {
		data += strings.Replace(commandBuy, "2024-01-01, 10:00:00", date, 1)
	}
	report, err := activity.ParseActivityReportReader(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	target, _ := time.Parse(time.DateOnly, "2024-02-01")
	out, err := captureStdout(func() error { AnalyzeSpecificTransaction(report, "xyz", target, 100); return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Total Reported Realized P/L: $98.00", "01/02/2024 |", "02/01/2024 |", "03/02/2024 |"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "03/03/2024 |") {
		t.Fatal("included day 31")
	}
}

func TestStaleReportShowsCoverageWarning(t *testing.T) {
	data := "Statement,Data,Period,\"January 1, 2024 - February 1, 2024\"\n" + commandHeader + commandBuy + commandSale
	report, err := activity.ParseActivityReportReader(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	today, _ := time.Parse(time.DateOnly, "2024-04-01")
	out, err := captureStdout(func() error { return printSafeRepurchaseDates(report, today, false) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "outside statement coverage") {
		t.Fatalf("missing coverage warning:\n%s", out)
	}
}
