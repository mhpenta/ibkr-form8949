package form8949

import (
	"strings"
	"testing"
)

const formHeader = "Header,Description,Date Acquired,Date Sold,Proceeds,Cost Basis,Code(s),Adjustment,Gain or (loss)\n"
const formRow = "Data,100 sh XYZ,01/01/2024,02/01/2024,1100,1000,,0,100\n"

func TestWorksheetCSVIntegrity(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		input := "\ufeffTitle,Worksheet for Form 8949,2024,\n" + formHeader + "Data,\"100 sh XYZ \"\"CLASS A\"\"\",01/01/2024,02/01/2024, 1100 , 1000 ,, , 100 \n"
		txs, year, err := ParseCSVReader(strings.NewReader(strings.ReplaceAll(input, "\n", ending)))
		if err != nil {
			t.Fatal(err)
		}
		if len(txs) != 1 || year != 2024 || txs[0].GainOrLoss != 100 || txs[0].Description != `100 sh XYZ "CLASS A"` {
			t.Fatalf("incorrect decoded worksheet: %+v / %d", txs, year)
		}
	}
}

func TestWorksheetRejectsIncompleteTotals(t *testing.T) {
	for name, input := range map[string]string{
		"bad amount":              formHeader + formRow + strings.Replace(formRow, "1100", "garbled", 1),
		"nonfinite":               formHeader + strings.Replace(formRow, "1100", "NaN", 1),
		"truncated":               formHeader + "Data,XYZ,01/01/2024\n",
		"bad date":                formHeader + strings.Replace(formRow, "02/01/2024", "nonsense", 1),
		"unknown sale date":       formHeader + strings.Replace(formRow, "02/01/2024", "VARIOUS", 1),
		"missing header":          formRow,
		"missing required column": strings.Replace(formHeader, "Cost Basis", "Other", 1) + formRow,
		"duplicate column":        strings.Replace(formHeader, "Cost Basis", "Proceeds", 1) + formRow,
		"empty":                   formHeader,
		"conflicting sections":    "Part,I\nBox,D\n" + formHeader + formRow,
	} {
		t.Run(name, func(t *testing.T) {
			txs, _, err := ParseCSVReader(strings.NewReader(input))
			if err == nil || txs != nil {
				t.Fatalf("accepted incomplete totals: %+v, %v", txs, err)
			}
			if name != "empty" && !strings.Contains(err.Error(), "line") {
				t.Fatalf("missing line number: %v", err)
			}
		})
	}
}

// Synthetic worksheet sections: no real Form 8949 export is available locally.
func TestWorksheetSectionsAndReorderedHeaders(t *testing.T) {
	input := "Title,Worksheet for Form 8949,2024,\nPart,I\nBox,A\n" + formHeader + formRow +
		"Part,II\nBox,D\nHeader,Description,Date Sold,Date Acquired,Cost Basis,Proceeds,Code(s),Gain or (loss),Adjustment\nData,200 sh XYZ,03/01/2024,VARIOUS,1000,1200,,200,0\n"
	txs, _, err := ParseCSVReader(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 2 || txs[0].IsLongTerm || !txs[1].IsLongTerm || !txs[1].DateAcquired.IsZero() || txs[1].Box != "D" {
		t.Fatalf("wrong section terms: %+v", txs)
	}
	a := AnalyzeTransactions(txs, 2024, 35, 45)
	if a.ShortTermGainOrLoss != 100 || a.LongTermGainOrLoss != 200 || a.UnknownTermGainOrLoss != 0 || len(a.PositionAnalysis) != 1 {
		t.Fatalf("incorrect aggregation: %+v", a)
	}
	// Explicit Part II also overrides a recent acquisition with an inherited term.
	txs, _, err = ParseCSVReader(strings.NewReader("Part,II\n" + formHeader + formRow))
	if err != nil || !txs[0].IsLongTerm {
		t.Fatalf("section did not override dates: %+v, %v", txs, err)
	}
}

func TestHoldingPeriodAnniversary(t *testing.T) {
	for _, tc := range []struct {
		acquired, sold string
		long           bool
	}{
		{"2023-03-01", "2024-02-29", false}, {"2023-01-15", "2024-01-15", false},
		{"2023-01-15", "2024-01-16", true}, {"2024-02-29", "2025-02-28", false},
		{"2024-02-29", "2025-03-01", true}, {"2024-01-01", "2024-12-31", false},
	} {
		row := []string{"Data", "XYZ", tc.acquired, tc.sold, "1100", "1000", "", "0", "100"}
		tx, err := parseTransaction(row)
		if err != nil || tx.IsLongTerm != tc.long {
			t.Errorf("%s -> %s: %+v, %v", tc.acquired, tc.sold, tx, err)
		}
	}
	txs, _, err := ParseCSVReader(strings.NewReader(formHeader + strings.Replace(formRow, "01/01/2024", "VARIOUS", 1)))
	if err != nil {
		t.Fatal(err)
	}
	a := AnalyzeTransactions(txs, 2024, 35, 45)
	if a.ShortTermGainOrLoss != 0 || a.LongTermGainOrLoss != 0 || a.UnknownTermGainOrLoss != 100 {
		t.Fatalf("unknown term was assigned: %+v", a)
	}
}

func TestSameDayDisposedLotIsUncertain(t *testing.T) {
	rows := formHeader + "Data,100 sh XYZ,01/01/2023,02/01/2024,900,1000,W,100,0\n" +
		"Data,50 sh XYZ,01/15/2024,02/01/2024,500,500,,0,0\n"
	txs, _, err := ParseCSVReader(strings.NewReader(rows))
	if err != nil {
		t.Fatal(err)
	}
	a := AnalyzeTransactions(txs, 2024, 35, 45)
	if len(a.SuspiciousWashSales) != 1 || !strings.Contains(a.SuspiciousWashSales[0].SuspiciousReason, "Same-day") {
		t.Fatalf("same-order lot was accepted: %+v", a.SuspiciousWashSales)
	}
}
