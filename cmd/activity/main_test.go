package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintAllTransactionsCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ibkr-activity")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build activity CLI: %v\n%s", err, out)
	}
	fixture := positionTradeHeader + `Trades,Data,Order,Stocks,USD,XYZ,"2024-02-05, 10:00:00",-2,12,24,-1,-21,2,C
Trades,Data,Order,Stocks,USD,XYZ,2024-02-01,2,10,-20,-1,21,0,O
Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,Proceeds,Comm in USD,MTM in USD,Code
Trades,Data,Order,Forex,USD,EUR.USD,2024-02-03,100,1.05235,-105.235,-1,0,
`
	path := filepath.Join(dir, "statement.csv")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, flags ...string) string {
		t.Helper()
		args := append([]string{"--file", path, "--print-all-transactions"}, flags...)
		out, err := exec.Command(binary, args...).Output()
		if err != nil {
			t.Fatalf("list trades: %v\n%s", err, out)
		}
		return string(out)
	}
	const header = "ALL TRANSACTIONS:\nDate/Time | Symbol | Type | Quantity | Currency | Price | Reported Realized P/L\n"
	const buy = "2024-02-01 | XYZ | BUY | 2 | USD | 10 | 0.00\n"
	const forex = "2024-02-03 | EUR.USD | BUY | 100 | USD | 1.05235 | --\n"
	const sell = "2024-02-05 10:00:00 | XYZ | SELL | 2 | USD | 12 | 2.00\n"
	if got := run(t); got != header+buy+forex+sell {
		t.Fatalf("unexpected statement list:\n%s", got)
	}
	if got := run(t, "--symbol", "xyz"); got != header+buy+sell {
		t.Fatalf("unexpected filtered list:\n%s", got)
	}
	if got := run(t, "--symbol", "missing"); got != "No trades found for symbol: MISSING\n" {
		t.Fatalf("unexpected output for a missing symbol:\n%s", got)
	}
	if got := run(t, "--symbol", "XYZ", "--analyze-position"); !strings.Contains(got, "XYZ POSITION SUMMARY") || !strings.Contains(got, "ALL TRANSACTIONS:") {
		t.Fatalf("position report lost its timeline:\n%s", got)
	}
	if out, err := exec.Command(binary, "--file", path, "--print-all-transactions", "--analyze-all").CombinedOutput(); err == nil ||
		!strings.Contains(string(out), "Use --print-all-transactions by itself or with --analyze-position") {
		t.Fatalf("expected an error for conflicting modes, got %v\n%s", err, out)
	}
}
