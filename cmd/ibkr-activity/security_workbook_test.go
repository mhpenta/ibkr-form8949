package main

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// readWorkbook returns each part of an .xlsx file by name.
func readWorkbook(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer z.Close()
	parts := make(map[string]string)
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(body)
	}
	return parts
}

func TestAnalyzeAllWritesTradeRowWorkbook(t *testing.T) {
	data := commandHeader + commandBuy + commandSale +
		"Trades,Data,Order,Stocks,USD,ABC,2024-02-02,10,5,5,-50,-1,51,0,0,O\n" +
		"Trades,Data,Order,Stocks,USD,ABC,2024-02-03,-10,4,4,40,-1,-51,-12,0,C\n"
	path := writeActivity(t, data)
	out := filepath.Join(t.TempDir(), "analysis.xlsx")

	stdout, err := captureStdout(func() error { return AnalyzeAllSecurities(path, out) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Spreadsheet written to "+out) {
		t.Fatalf("missing spreadsheet notice:\n%s", stdout)
	}

	parts := readWorkbook(t, out)
	if !strings.Contains(parts["xl/workbook.xml"], `<sheet name="Securities"`) || !strings.Contains(parts["xl/workbook.xml"], `<sheet name="Summary"`) {
		t.Fatalf("unexpected sheets:\n%s", parts["xl/workbook.xml"])
	}
	securities := parts["xl/worksheets/sheet1.xml"]
	// Best performer first; P/L is numeric, not text.
	xyz := strings.Index(securities, ">XYZ<")
	abc := strings.Index(securities, ">ABC<")
	if xyz < 0 || abc < 0 || xyz > abc {
		t.Fatalf("securities not sorted best first:\n%s", securities)
	}
	for _, want := range []string{`<c r="C2" s="2"><v>98</v></c>`, `<c r="C3" s="2"><v>-12</v></c>`, `<autoFilter ref="A1:I3"/>`} {
		if !strings.Contains(securities, want) {
			t.Fatalf("securities sheet missing %s:\n%s", want, securities)
		}
	}
	summary := parts["xl/worksheets/sheet2.xml"]
	for _, want := range []string{"STOCK TRADE-ROW PERFORMANCE REPORT", "Securities traded", `<v>86</v>`, "Not tax advice"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary sheet missing %s:\n%s", want, summary)
		}
	}
}

func TestAnalyzeAllWritesMarkToMarketWorkbook(t *testing.T) {
	data := `Statement,Data,Period,"January 1, 2025 - December 31, 2025"
Account Information,Data,Base Currency,USD
Change in NAV,Data,Starting Value,10000
Change in NAV,Data,Mark-to-Market,-2340
Change in NAV,Data,Ending Value,7660
Mark-to-Market Performance Summary,Header,Asset Category,Symbol,Prior Quantity,Current Quantity,Prior Price,Current Price,Mark-to-Market P/L Position,Mark-to-Market P/L Transaction,Mark-to-Market P/L Commissions,Mark-to-Market P/L Other,Mark-to-Market P/L Total,Code
Mark-to-Market Performance Summary,Data,Stocks,AAPL,100,0,150.0000,--,-1200,-300.25,-1.5,0,-1501.75,
Mark-to-Market Performance Summary,Data,Equity and Index Options,MSFT 19DEC25 500 C,0,0,--,--,-800.5,-50.25,12.5,0,-838.25,
Mark-to-Market Performance Summary,Data,Total (All Assets),,,,,,-2000.5,-350.5,11,0,-2340,
`
	path := writeActivity(t, data)
	out := filepath.Join(t.TempDir(), "mtm.xlsx")
	if _, err := captureStdout(func() error { return AnalyzeAllSecurities(path, out) }); err != nil {
		t.Fatal(err)
	}

	parts := readWorkbook(t, out)
	for _, name := range []string{"Securities", "By Asset Category", "Summary"} {
		if !strings.Contains(parts["xl/workbook.xml"], `<sheet name="`+name+`"`) {
			t.Fatalf("missing sheet %s:\n%s", name, parts["xl/workbook.xml"])
		}
	}
	securities := parts["xl/worksheets/sheet1.xml"]
	if !strings.Contains(securities, "Total P/L (USD)") || strings.Contains(securities, "Total (All Assets)") {
		t.Fatalf("securities sheet should list detail rows only:\n%s", securities)
	}
	if strings.Index(securities, ">MSFT 19DEC25 500 C<") > strings.Index(securities, ">AAPL<") {
		t.Fatalf("securities not sorted best first:\n%s", securities)
	}
	summary := parts["xl/worksheets/sheet3.xml"]
	for _, want := range []string{"Account NAV", "Starting NAV", `<v>-2340</v>`, "IBKR Total (All Assets)"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary sheet missing %s:\n%s", want, summary)
		}
	}
}

func TestValidSpreadsheetPath(t *testing.T) {
	for path, ok := range map[string]bool{"out.xlsx": true, "OUT.XLSX": true, "out.csv": false, "xlsx": false} {
		if err := validSpreadsheetPath(path); (err == nil) != ok {
			t.Fatalf("validSpreadsheetPath(%q) error = %v, want ok=%v", path, err, ok)
		}
	}
}
