package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func readParts(t *testing.T, book *Workbook) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := book.Write(&buf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
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
		// Every part must be well-formed XML.
		dec := xml.NewDecoder(bytes.NewReader(body))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed XML: %v\n%s", f.Name, err, body)
			}
		}
		parts[f.Name] = string(body)
	}
	return parts
}

func TestWorkbookParts(t *testing.T) {
	var book Workbook
	sheet := book.AddSheet("Securities")
	sheet.Header("Symbol", "P/L", "Trades")
	sheet.Widths(12, 14, 8)
	sheet.Row(Text("AT&T <x>"), Money(-12.5), Int(3))
	sheet.Row(Text("ZZZ"), Money(1e6), Int(1))
	notes := book.AddSheet("Summary: notes/2024")
	notes.Row(Title("Report"))
	notes.Blank()
	notes.Row(Text("Share"), Percent(0.25))

	parts := readParts(t, &book)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if _, ok := parts[name]; !ok {
			t.Fatalf("missing part %s", name)
		}
	}

	first := parts["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`<c r="A2" t="inlineStr"><is><t xml:space="preserve">AT&amp;T &lt;x&gt;</t></is></c>`,
		`<c r="B2" s="2"><v>-12.5</v></c>`,
		`<c r="B3" s="2"><v>1e+06</v></c>`,
		`<c r="C3" s="3"><v>1</v></c>`,
		`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`,
		`<autoFilter ref="A1:C3"/>`,
		`<col min="1" max="1" width="12" customWidth="1"/>`,
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("sheet1 missing %s:\n%s", want, first)
		}
	}

	second := parts["xl/worksheets/sheet2.xml"]
	if strings.Contains(second, "autoFilter") || strings.Contains(second, "frozen") {
		t.Fatalf("sheet without a header should not freeze or filter:\n%s", second)
	}
	if strings.Contains(second, `<row r="2">`) || !strings.Contains(second, `<c r="B3" s="4"><v>0.25</v></c>`) {
		t.Fatalf("blank row should be skipped but keep later row numbers:\n%s", second)
	}

	workbook := parts["xl/workbook.xml"]
	if !strings.Contains(workbook, `<sheet name="Summary- notes-2024" sheetId="2" r:id="rId2"/>`) {
		t.Fatalf("sheet name not sanitized:\n%s", workbook)
	}
	if !strings.Contains(workbook, `'Securities'!$A$1:$C$3`) {
		t.Fatalf("missing filter database name:\n%s", workbook)
	}
}

func TestColumnName(t *testing.T) {
	for n, want := range map[int]string{1: "A", 26: "Z", 27: "AA", 52: "AZ", 703: "AAA"} {
		if got := columnName(n); got != want {
			t.Fatalf("columnName(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestEmptyWorkbookFails(t *testing.T) {
	var book Workbook
	if err := book.Write(io.Discard); err == nil {
		t.Fatal("expected an error for a workbook without sheets")
	}
}
