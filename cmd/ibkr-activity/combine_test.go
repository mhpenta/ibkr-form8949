package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mhpenta/ibkr-form8949/internal/activity"
)

func TestParseArgsCollectsFilesAnywhere(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	var files statementFiles
	flags.Var(&files, "file", "")
	symbol := flags.String("symbol", "", "")
	// What a shell passes for: --file *.csv --symbol XYZ --file extra.csv
	extra := parseArgs(flags, []string{"--file", "a.csv", "b.csv", "c.csv", "--symbol", "XYZ", "--file", "d.csv"})
	files = append(files, extra...)
	if want := (statementFiles{"a.csv", "d.csv", "b.csv", "c.csv"}); !reflect.DeepEqual(files, want) || *symbol != "XYZ" {
		t.Fatalf("files %v, symbol %q", files, *symbol)
	}
}

func TestPositionCombinesStatements(t *testing.T) {
	dir := t.TempDir()
	write := func(name, period, generated, trades string) string {
		path := filepath.Join(dir, name)
		text := "Statement,Data,Period,\"" + period + "\"\nStatement,Data,WhenGenerated,\"" + generated + ", 08:00:00 EST\"\n" + positionTradeHeader + trades
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	buy := "Trades,Data,Order,Stocks,USD,XYZ,2024-03-15,50,12,-600,-1,601,0,O\n"
	q1 := write("q1.csv", "January 1, 2024 - March 31, 2024", "2024-04-02", "Trades,Data,Order,Stocks,USD,XYZ,2024-01-03,100,10,-1000,-1,1001,0,O\n"+buy)
	q2 := write("q2.csv", "March 1, 2024 - June 30, 2024", "2024-07-02", buy)
	h2 := write("h2.csv", "August 1, 2024 - December 31, 2024", "2025-01-02", "Trades,Data,Order,Stocks,USD,XYZ,2024-08-05,-150,9,1350,-1,-1602,-253,C\n")
	out := filepath.Join(dir, "xyz.xlsx")

	stdout, err := captureStdout(func() error { return analyzeSymbolPosition([]string{h2, q2, q1}, "XYZ", false, out) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Combined 3 statements",
		"q1.csv: January 1, 2024 - March 31, 2024, 1 of 2 trades used",
		"Average Buy Price: $10.6667",
		"Statement coverage: 2024-01-01 through 2024-06-30; 2024-08-01 through 2024-12-31",
		"No statement covers 2024-07-01 through 2024-07-31",
		"Statement does not cover the full window",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q:\n%s", want, stdout)
		}
	}
	parts := readWorkbook(t, out)
	if summary := parts["xl/worksheets/sheet1.xml"]; !strings.Contains(summary, ">Statements combined<") || !strings.Contains(summary, ">2024-07-01 through 2024-07-31<") {
		t.Fatalf("summary should list statements and gaps:\n%s", summary)
	}
	if transactions := parts["xl/worksheets/sheet2.xml"]; !strings.Contains(transactions, ">Statement<") || !strings.Contains(transactions, ">q2.csv<") {
		t.Fatalf("transactions should name each trade's statement:\n%s", transactions)
	}
}

func TestLoadStatementsRefusesMixedAccounts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, account string) string {
		path := filepath.Join(dir, name)
		text := "Statement,Data,Period,\"January 1, 2024 - December 31, 2024\"\nAccount Information,Data,Account," + account + "\n" + positionTradeHeader
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	files := []string{write("a.csv", "U111"), write("b.csv", "U222")}
	_, err := loadStatements(files)
	var mismatch *activity.AccountMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected an account mismatch, got %v", err)
	}
	mergeOptions.AllowMultipleAccounts = true
	defer func() { mergeOptions.AllowMultipleAccounts = false }()
	if _, err := captureStdout(func() error { _, err := loadStatements(files); return err }); err != nil {
		t.Fatalf("allowed accounts should combine: %v", err)
	}
}
