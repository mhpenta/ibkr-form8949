package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeSymbolPositionReportsTotalSharesTraded(t *testing.T) {
	csvData := `Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
Trades,Data,Order,Stocks,USD,XYZ,"2024-01-01, 10:00:00",100,10,10,-1000,-1,1000,0,0,
Trades,Data,Order,Stocks,USD,XYZ,"2024-02-01, 10:00:00",-100,11,11,1100,-1,1000,100,0,
`

	filePath := filepath.Join(t.TempDir(), "activity.csv")
	if err := os.WriteFile(filePath, []byte(csvData), 0600); err != nil {
		t.Fatalf("failed to write activity CSV: %v", err)
	}

	output, err := captureStdout(func() error {
		return AnalyzeSymbolPosition(filePath, "XYZ")
	})
	if err != nil {
		t.Fatalf("AnalyzeSymbolPosition failed: %v", err)
	}

	if !strings.Contains(output, "Total Shares Traded: 200") {
		t.Fatalf("expected total shares traded to use share volume, output:\n%s", output)
	}
	if !strings.Contains(output, "Average P/L per Share: $0.50") {
		t.Fatalf("expected average P/L per share to use share volume, output:\n%s", output)
	}
}

func TestAnalyzeSymbolPositionUsesMTMCurrentQuantityForOpeningPositions(t *testing.T) {
	csvData := `Statement,Data,Period,"January 1, 2025 - December 31, 2025"
Account Information,Data,Base Currency,USD
Mark-to-Market Performance Summary,Header,Asset Category,Symbol,Prior Quantity,Current Quantity,Prior Price,Current Price,Mark-to-Market P/L Position,Mark-to-Market P/L Transaction,Mark-to-Market P/L Commissions,Mark-to-Market P/L Other,Mark-to-Market P/L Total,Code
Mark-to-Market Performance Summary,Data,Stocks,MSFT,1000,0,400.0000,--,-5000,250.5,-2.25,10.5,-4741.25,
Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,C. Price,Proceeds,Comm/Fee,Basis,Realized P/L,MTM P/L,Code
Trades,Data,Order,Stocks,USD,MSFT,"2025-01-10, 10:00:00",-1000,405,405,405000,-1,400000,4999,0,
`

	filePath := filepath.Join(t.TempDir(), "activity.csv")
	if err := os.WriteFile(filePath, []byte(csvData), 0600); err != nil {
		t.Fatalf("failed to write activity CSV: %v", err)
	}

	output, err := captureStdout(func() error {
		return AnalyzeSymbolPosition(filePath, "MSFT")
	})
	if err != nil {
		t.Fatalf("AnalyzeSymbolPosition failed: %v", err)
	}

	for _, want := range []string{
		"Net Period Trade Qty: -1000",
		"IBKR Prior Quantity: 1000",
		"IBKR Current Quantity: 0",
		"Position fully liquidated",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, output:\n%s", want, output)
		}
	}

	if strings.Contains(output, "Position has open shares") {
		t.Fatalf("expected MTM current quantity to override net period trade quantity, output:\n%s", output)
	}
}

func TestAnalyzeSymbolPositionWithSnapshotOnly(t *testing.T) {
	const header = "Mark-to-Market Performance Summary,Header,Asset Category,Symbol,Prior Quantity,Current Quantity,Mark-to-Market P/L Total\n"
	const stock = "Mark-to-Market Performance Summary,Data,Stocks,XYZ,"
	for _, tt := range []struct {
		name string
		rows string
		want string
	}{
		{"held throughout period", stock + "100,100,0\n", "IBKR Current Quantity: 100"},
		{"unavailable ending quantity", stock + "100,--,0\n", "Current position unknown"},
		{"one unavailable ending quantity", stock + "0,0,0\n" + stock + "100,,0\n", "Current position unknown"},
		{"offsetting open positions", stock + "100,100,0\n" + stock + "0,-100,0\n", "IBKR Net Current Quantity: 0\nPosition has open shares"},
		{"fractional holding", stock + "0,0.0000001,0\n", "Position has open shares"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := runPositionFixture(t, header+tt.rows)
			if !strings.Contains(out, tt.want) || !strings.Contains(out, "No stock transactions found") ||
				strings.Contains(out, "fully liquidated") || strings.Contains(out, "First Transaction:") {
				t.Fatalf("expected %q and no transaction dates or liquidation claim:\n%s", tt.want, out)
			}
		})
	}
}

const positionTradeHeader = "Trades,Header,DataDiscriminator,Asset Category,Currency,Symbol,Date/Time,Quantity,T. Price,Proceeds,Comm/Fee,Basis,Realized P/L,Code\n"

func TestAnalyzeSymbolPositionReportsShortCoverEvidenceSeparately(t *testing.T) {
	const longSale = "Trades,Data,Order,Stocks,USD,XYZ,2024-02-01,-100,10,1000,-1,-1101,-102,C\n"
	const openShort = "Trades,Data,Order,Stocks,USD,XYZ,2024-02-02,-100,10,1000,-1,-999,0,O\n"
	const cover = "Trades,Data,Order,Stocks,USD,XYZ,2024-02-05,100,12,-1200,-1,999,"
	for _, tt := range []struct {
		name     string
		trades   string
		want     string
		wantLong bool
	}{
		{"short-cover loss", openShort + cover + "-202,C\n", "Latest reported short-cover loss or LD adjustment: 2024-02-05", false},
		{"LD-coded cover after a long loss", longSale + openShort + cover + "0,C;LD\n", "Latest reported short-cover loss or LD adjustment: 2024-02-05", true},
		{"LD-coded opening trade", cover + "0,O;LD\n", "Latest loss or LD adjustment requiring trade classification: 2024-02-05", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := runPositionFixture(t, positionTradeHeader+tt.trades)
			if !strings.Contains(out, tt.want) || strings.Contains(out, "No net loss") ||
				strings.Contains(out, "Latest reported long loss-sale window: 2024-01-02 through 2024-03-02") != tt.wantLong ||
				strings.Contains(out, "First date outside that forward window:") != tt.wantLong {
				t.Fatalf("expected %q, long-sale window = %t:\n%s", tt.want, tt.wantLong, out)
			}
		})
	}
}

func TestAnalyzeSymbolPositionSeparatesAmbiguousSameDayBuys(t *testing.T) {
	const sale = "Trades,Data,Order,Stocks,USD,XYZ,%s,-100,10,1000,-1,-1101,-102,C\n"
	const buy = "Trades,Data,Order,Stocks,USD,XYZ,%s,100,12,-1200,-1,1201,0,O\n"
	for _, tt := range []struct {
		name, sales, buyDate, want string
		uncertain                  bool
	}{
		{"date-only sale", fmt.Sprintf(sale, "2024-02-05"), `"2024-02-05, 11:00:00"`, "2024-02-05 11:00:00 | BUY", true},
		{"date-only sale and timed sale", fmt.Sprintf(sale, "2024-02-05") + fmt.Sprintf(sale, `"2024-02-05, 10:00:00"`), `"2024-02-05, 11:00:00"`, "2024-02-05 11:00:00 | BUY", true},
		{"date-only buy", fmt.Sprintf(sale, `"2024-02-05, 10:00:00"`), "2024-02-05", "2024-02-05 | BUY", true},
		{"equal timestamps", fmt.Sprintf(sale, `"2024-02-05, 10:00:00"`), `"2024-02-05, 10:00:00"`, "2024-02-05 10:00:00 | BUY", true},
		{"buy after timed sale", fmt.Sprintf(sale, `"2024-02-05, 10:00:00"`), `"2024-02-05, 11:00:00"`, "2024-02-05 11:00:00 | BUY", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := runPositionFixture(t, positionTradeHeader+tt.sales+fmt.Sprintf(buy, tt.buyDate)+fmt.Sprintf(buy, "2024-02-06"))
			_, after, found := strings.Cut(out, "TRANSACTIONS AFTER LAST SALE (2024-02-05):")
			if !found {
				t.Fatalf("missing post-sale section:\n%s", out)
			}
			after, uncertain, _ := strings.Cut(after, "SAME-DAY BUYS WITH UNCERTAIN ORDER RELATIVE TO LAST SALE")
			if !strings.Contains(after, "2024-02-06 | BUY") || strings.Contains(after, tt.want) == tt.uncertain ||
				strings.Contains(uncertain, tt.want) != tt.uncertain {
				t.Fatalf("wrong section for %q:\n%s", tt.want, out)
			}
		})
	}
}

func runPositionFixture(t *testing.T, csvData string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "activity.csv")
	if err := os.WriteFile(path, []byte(csvData), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(func() error { return AnalyzeSymbolPosition(path, "XYZ") })
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func captureStdout(fn func() error) (string, error) {
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}

	os.Stdout = writer
	runErr := fn()
	closeErr := writer.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, reader)
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}

	return buf.String(), runErr
}
