package activity

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const mergeTradeHeader = "Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,Proceeds,Comm/Fee,Basis,Realized P/L,Code\n"
const mergeMTMHeader = "Mark-to-Market Performance Summary,Header,Asset Category,Symbol,Prior Quantity,Current Quantity,Prior Price,Current Price,Mark-to-Market P/L Position,Mark-to-Market P/L Transaction,Mark-to-Market P/L Commissions,Mark-to-Market P/L Other,Mark-to-Market P/L Total,Code\n"

// statement builds a parsed statement named name for period, generated on the given day.
func statement(t *testing.T, name, period, generated, body string) *ActivityReport {
	t.Helper()
	text := fmt.Sprintf("Statement,Data,Period,\"%s\"\nStatement,Data,WhenGenerated,\"%s, 08:00:00 EST\"\nAccount Information,Data,Base Currency,USD\n%s",
		period, generated, body)
	r, err := ParseActivityReportReader(strings.NewReader(text))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	r.Sources[0].Name = name
	return r
}

func buyRow(day string, quantity, price float64) string {
	return fmt.Sprintf("Trades,Data,Order,Stocks,USD,XYZ,%s,%g,%g,%g,-1,%g,0,O\n", day, quantity, price, -quantity*price, quantity*price+1)
}

func mtmRow(prior, current, total float64) string {
	return fmt.Sprintf("Mark-to-Market Performance Summary,Data,Stocks,XYZ,%g,%g,10,11,0,0,0,0,%g,\n", prior, current, total)
}

func hasWarning(r *ActivityReport, part string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w, part) {
			return true
		}
	}
	return false
}

func TestMergeOverlapUsesNewestStatementOnce(t *testing.T) {
	q1 := statement(t, "q1.csv", "January 1, 2024 - March 31, 2024", "2024-04-02",
		mergeMTMHeader+mtmRow(0, 150, 10)+mergeTradeHeader+buyRow("2024-01-03", 100, 10)+buyRow("2024-03-15", 50, 12))
	q2 := statement(t, "q2.csv", "March 1, 2024 - June 30, 2024", "2024-07-02",
		mergeMTMHeader+mtmRow(100, 150, 20)+mergeTradeHeader+buyRow("2024-03-15", 50, 12))
	// Input order does not matter: statements are sorted by period.
	r, err := MergeReports([]*ActivityReport{q2, q1}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Trades) != 2 || r.TradesBySymbol["XYZ"][1].Source != "q2.csv" || r.TradesBySymbol["XYZ"][0].Source != "q1.csv" {
		t.Fatalf("overlapping day should come from the newer statement once: %+v", r.Trades)
	}
	if r.Sources[0].Name != "q1.csv" || r.Sources[0].TradesUsed != 1 || r.Sources[0].Trades != 2 || r.Sources[1].TradesUsed != 1 {
		t.Fatalf("sources: %+v", r.Sources)
	}
	if !hasWarning(r, "q1.csv and q2.csv overlap from 2024-03-01 through 2024-03-31") || hasWarning(r, "disagree") {
		t.Fatalf("warnings: %v", r.Warnings)
	}
	if r.Period != "January 1, 2024 - June 30, 2024" || len(r.Gaps()) != 0 || !r.CoversDates(day("2024-01-01"), day("2024-06-30")) {
		t.Fatalf("coverage: %s %+v", r.Period, r.Coverage)
	}
	rows := r.MarkToMarketPerformanceBySymbol["XYZ"]
	if len(rows) != 1 || rows[0].PriorQuantity != 0 || rows[0].CurrentQuantity != 150 || rows[0].TotalPL != 0 || !r.MarkToMarketPLUnavailable {
		t.Fatalf("overlapping summaries keep quantities but not P/L: %+v", rows)
	}
	if !r.StatementDate.Equal(q2.StatementDate) {
		t.Fatalf("statement date should be the newest: %v", r.StatementDate)
	}
}

func TestMergeWarnsWhenOverlappingTradesDisagree(t *testing.T) {
	older := statement(t, "a.csv", "January 1, 2024 - March 31, 2024", "2024-05-01", mergeTradeHeader+buyRow("2024-03-15", 50, 12))
	newer := statement(t, "b.csv", "March 1, 2024 - June 30, 2024", "2024-07-02", mergeTradeHeader+buyRow("2024-03-15", 60, 12))
	r, err := MergeReports([]*ActivityReport{older, newer}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Trades) != 1 || r.Trades[0].Quantity != 60 || !hasWarning(r, "a.csv and b.csv disagree about trades from 2024-03-01 through 2024-03-31") {
		t.Fatalf("trades %+v, warnings %v", r.Trades, r.Warnings)
	}
	// A full-period statement generated later owns every day it covers.
	full := statement(t, "full.csv", "January 1, 2024 - June 30, 2024", "2024-08-01", mergeTradeHeader+buyRow("2024-03-15", 55, 12))
	r, err = MergeReports([]*ActivityReport{older, newer, full}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Trades) != 1 || r.Trades[0].Source != "full.csv" {
		t.Fatalf("newest covering statement should win: %+v", r.Trades)
	}
}

func TestMergeGapsBreakCoverage(t *testing.T) {
	h1 := statement(t, "h1.csv", "January 1, 2024 - June 30, 2024", "2024-07-02",
		mergeMTMHeader+mtmRow(0, 100, 10)+mergeTradeHeader+buyRow("2024-01-03", 100, 10))
	h2 := statement(t, "h2.csv", "August 1, 2024 - December 31, 2024", "2025-01-02",
		mergeMTMHeader+mtmRow(100, 100, 5))
	r, err := MergeReports([]*ActivityReport{h1, h2}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gaps := r.Gaps()
	if len(gaps) != 1 || gaps[0].String() != "2024-07-01 through 2024-07-31" || !hasWarning(r, "No statement covers 2024-07-01 through 2024-07-31") {
		t.Fatalf("gaps %v, warnings %v", gaps, r.Warnings)
	}
	if r.CoversDates(day("2024-06-15"), day("2024-08-15")) || !r.CoversDates(day("2024-08-01"), day("2024-08-31")) {
		t.Fatal("a window across the gap is not covered; one inside a statement is")
	}
	rows := r.MarkToMarketPerformanceBySymbol["XYZ"]
	if r.MarkToMarketPLUnavailable || rows[0].TotalPL != 15 || rows[0].PriorQuantity != 0 || rows[0].CurrentQuantity != 100 {
		t.Fatalf("non-overlapping summaries add up P/L: %+v", rows)
	}
	if r.ChangeInNAV.HasData() {
		t.Fatal("no NAV data to combine")
	}
}

func TestMergeChangeInNAVBackToBack(t *testing.T) {
	nav := func(start, mtm, deposits, end float64) string {
		return fmt.Sprintf("Change in NAV,Header,Field Name,Field Value\nChange in NAV,Data,Starting Value,%g\nChange in NAV,Data,Mark-to-Market,%g\nChange in NAV,Data,Deposits & Withdrawals,%g\nChange in NAV,Data,Ending Value,%g\n",
			start, mtm, deposits, end) + mergeTradeHeader
	}
	a := statement(t, "a.csv", "January 1, 2024 - June 30, 2024", "2024-07-02", nav(1000, 50, 100, 1150))
	b := statement(t, "b.csv", "July 1, 2024 - December 31, 2024", "2025-01-02", nav(1150, -20, 0, 1130))
	r, err := MergeReports([]*ActivityReport{a, b}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := r.ChangeInNAV
	if n.StartingValue != 1000 || n.EndingValue != 1130 || n.MarkToMarket != 30 || n.DepositsWithdrawals != 100 {
		t.Fatalf("NAV: %+v", n)
	}
	c := statement(t, "c.csv", "August 1, 2024 - December 31, 2024", "2025-01-02", nav(1150, -20, 0, 1130))
	if r, err = MergeReports([]*ActivityReport{a, c}, MergeOptions{}); err != nil || r.ChangeInNAV.HasData() || !hasWarning(r, "Change in NAV is not combined") {
		t.Fatalf("a gap leaves NAV uncombined: %v %+v", err, r.Warnings)
	}
}

func TestMergeOpeningPositionComesFromFirstTradeStatement(t *testing.T) {
	sale := "Trades,Data,Order,Stocks,USD,XYZ,2024-08-05,-50,9,450,-1,-501,-52,C\n"
	h1 := statement(t, "h1.csv", "January 1, 2024 - June 30, 2024", "2024-07-02",
		mergeMTMHeader+mtmRow(40, 140, 0)+mergeTradeHeader+buyRow("2024-02-01", 100, 10))
	h2 := statement(t, "h2.csv", "July 1, 2024 - December 31, 2024", "2025-01-02",
		mergeMTMHeader+mtmRow(140, 90, 0)+mergeTradeHeader+sale)
	r, err := MergeReports([]*ActivityReport{h2, h1}, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	key := tradeInventoryKey(r.TradesBySymbol["XYZ"][0])
	if r.OpeningPositions[key] != 40 || len(r.OpeningPositions) != 1 {
		t.Fatalf("opening position should be the first statement's: %v", r.OpeningPositions)
	}
}

func TestMergeRejectsIncompatibleStatements(t *testing.T) {
	a := statement(t, "a.csv", "January 1, 2024 - June 30, 2024", "2024-07-02", mergeTradeHeader+buyRow("2024-01-03", 1, 1))
	noPeriod, err := ParseActivityReportReader(strings.NewReader(mergeTradeHeader + buyRow("2024-01-03", 1, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MergeReports([]*ActivityReport{a, noPeriod}, MergeOptions{}); err == nil || !strings.Contains(err.Error(), "statement 2 has no statement period") {
		t.Fatalf("expected a missing-period error, got %v", err)
	}
	eur := statement(t, "eur.csv", "July 1, 2024 - December 31, 2024", "2025-01-02", mergeTradeHeader)
	eur.BaseCurrency = "EUR"
	if _, err := MergeReports([]*ActivityReport{a, eur}, MergeOptions{}); err == nil || !strings.Contains(err.Error(), "different base currencies") {
		t.Fatalf("expected a base-currency error, got %v", err)
	}
}

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func accountStatement(t *testing.T, name, account, period, generated, body string) *ActivityReport {
	t.Helper()
	return statement(t, name, period, generated, "Account Information,Data,Account,"+account+"\n"+body)
}

func TestMergeRefusesDifferentAccountsByDefault(t *testing.T) {
	a := accountStatement(t, "a.csv", "U111", "January 1, 2024 - December 31, 2024", "2025-01-02", mergeTradeHeader+buyRow("2024-03-01", 100, 10))
	b := accountStatement(t, "b.csv", "U222", "January 1, 2024 - June 30, 2024", "2025-01-03", mergeTradeHeader)
	b2 := accountStatement(t, "b2.csv", "U222", "July 1, 2024 - December 31, 2024", "2025-01-03", mergeTradeHeader)
	_, err := MergeReports([]*ActivityReport{a, b, b2}, MergeOptions{})
	var mismatch *AccountMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected an account mismatch, got %v", err)
	}
	for _, want := range []string{
		"These statements come from 2 different accounts, so they were not combined:",
		"  Account U111: a.csv\n",
		"  Account U222: b.csv, b2.csv\n",
		"run again with only that account's statements",
		"add --allow-multiple-accounts",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message missing %q:\n%s", want, err)
		}
	}
	// The same account in several files is fine.
	if _, err := MergeReports([]*ActivityReport{b, b2}, MergeOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeAllowedAccountsCombinePerAccount(t *testing.T) {
	a := accountStatement(t, "a.csv", "U111", "January 1, 2024 - December 31, 2024", "2025-01-02",
		mergeMTMHeader+mtmRow(0, 100, 0)+mergeTradeHeader+buyRow("2024-03-01", 100, 10))
	b1 := accountStatement(t, "b1.csv", "U222", "February 1, 2024 - June 30, 2024", "2024-07-02",
		mergeMTMHeader+mtmRow(5, 15, 0)+mergeTradeHeader+buyRow("2024-05-01", 10, 50))
	b2 := accountStatement(t, "b2.csv", "U222", "August 1, 2024 - December 31, 2024", "2025-01-03",
		mergeMTMHeader+mtmRow(15, 20, 0)+mergeTradeHeader+buyRow("2024-09-01", 5, 50))
	r, err := MergeReports([]*ActivityReport{a, b1, b2}, MergeOptions{AllowMultipleAccounts: true})
	if err != nil {
		t.Fatal(err)
	}
	// Same-period statements from different accounts are not overlaps.
	if len(r.Trades) != 3 || hasWarning(r, "overlap") || !hasWarning(r, "Combining 2 accounts") {
		t.Fatalf("trades %+v, warnings %v", r.Trades, r.Warnings)
	}
	if !hasWarning(r, "No statement for account U222 covers 2024-07-01 through 2024-07-31") {
		t.Fatalf("gaps are reported per account: %v", r.Warnings)
	}
	// Covered only where every account is covered.
	want := []DateRange{{day("2024-02-01"), day("2024-06-30")}, {day("2024-08-01"), day("2024-12-31")}}
	if !reflect.DeepEqual(r.Coverage, want) || r.CoversDates(day("2024-01-10"), day("2024-01-10")) || r.Period != "January 1, 2024 - December 31, 2024" {
		t.Fatalf("coverage %v, period %s", r.Coverage, r.Period)
	}
	// Opening from each account's first statement, ending from its last, added together.
	row := r.MarkToMarketPerformanceBySymbol["XYZ"][0]
	if row.PriorQuantity != 5 || row.CurrentQuantity != 120 {
		t.Fatalf("quantities: %+v", row)
	}
}
