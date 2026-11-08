// Package xlsx writes small Office Open XML workbooks using only the standard library.
//
// It supports exactly what the reports need: several sheets, text and numeric cells,
// a bold header row that stays frozen with a filter, a few number formats, and column
// widths. The files open in Excel, Google Sheets, Numbers and LibreOffice.
package xlsx

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// Style selects a cell's formatting. The values index cellXfs in styles.xml.
type Style int

const (
	StyleDefault Style = iota
	StyleBold
	StyleMoney   // #,##0.00 with negatives in red
	StyleInteger // #,##0
	StylePercent // 0.0%
	StyleTitle   // bold, larger
)

// Cell is one spreadsheet cell: text, a number, or empty.
type Cell struct {
	text    string
	number  float64
	numeric bool
	style   Style
}

// Text returns a plain text cell.
func Text(s string) Cell { return Cell{text: s} }

// Bold returns a bold text cell.
func Bold(s string) Cell { return Cell{text: s, style: StyleBold} }

// Title returns a large bold text cell.
func Title(s string) Cell { return Cell{text: s, style: StyleTitle} }

// Number returns a numeric cell with the General format.
func Number(v float64) Cell { return Cell{number: v, numeric: true} }

// Money returns a numeric cell formatted with two decimals and thousands separators.
func Money(v float64) Cell { return Cell{number: v, numeric: true, style: StyleMoney} }

// Int returns a numeric cell formatted as a whole number.
func Int(v int) Cell { return Cell{number: float64(v), numeric: true, style: StyleInteger} }

// Percent returns a numeric cell for a fraction (0.25 shows as 25.0%).
func Percent(fraction float64) Cell {
	return Cell{number: fraction, numeric: true, style: StylePercent}
}

// Empty returns a blank cell.
func Empty() Cell { return Cell{} }

// Workbook is a set of sheets written as one .xlsx file.
type Workbook struct {
	sheets []*Sheet
}

// Sheet is one worksheet tab.
type Sheet struct {
	name      string
	rows      [][]Cell
	widths    []float64
	headerRow int // 1-based row of the table header; 0 means none
}

// AddSheet appends a worksheet. Names longer than 31 characters or containing
// characters spreadsheets reject are cleaned up.
func (w *Workbook) AddSheet(name string) *Sheet {
	s := &Sheet{name: sheetName(name, len(w.sheets)+1)}
	w.sheets = append(w.sheets, s)
	return s
}

// Row appends a row of cells.
func (s *Sheet) Row(cells ...Cell) {
	s.rows = append(s.rows, cells)
}

// Blank appends an empty row.
func (s *Sheet) Blank() {
	s.rows = append(s.rows, nil)
}

// Header appends a bold header row. The sheet freezes everything above and including
// it, and puts a filter on the table that starts there. Only the first call counts.
func (s *Sheet) Header(names ...string) {
	cells := make([]Cell, len(names))
	for i, name := range names {
		cells[i] = Bold(name)
	}
	s.rows = append(s.rows, cells)
	if s.headerRow == 0 {
		s.headerRow = len(s.rows)
	}
}

// Widths sets column widths in characters, starting at column A.
func (s *Sheet) Widths(widths ...float64) {
	s.widths = widths
}

// Save writes the workbook to path.
func (w *Workbook) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := w.Write(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Write writes the workbook as .xlsx bytes.
func (w *Workbook) Write(out io.Writer) error {
	if len(w.sheets) == 0 {
		return fmt.Errorf("xlsx: workbook has no sheets")
	}
	z := zip.NewWriter(out)
	files := []struct {
		name string
		body func(io.Writer) error
	}{
		{"[Content_Types].xml", w.contentTypes},
		{"_rels/.rels", staticPart(rootRels)},
		{"xl/workbook.xml", w.workbook},
		{"xl/_rels/workbook.xml.rels", w.workbookRels},
		{"xl/styles.xml", staticPart(styles)},
	}
	for i, sheet := range w.sheets {
		sheet := sheet
		files = append(files, struct {
			name string
			body func(io.Writer) error
		}{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheet.write})
	}
	for _, file := range files {
		part, err := z.Create(file.name)
		if err != nil {
			return err
		}
		buf := bufio.NewWriter(part)
		if err := file.body(buf); err != nil {
			return err
		}
		if err := buf.Flush(); err != nil {
			return err
		}
	}
	return z.Close()
}

func staticPart(body string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := io.WriteString(w, body)
		return err
	}
}

func (w *Workbook) contentTypes(out io.Writer) error {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := range w.sheets {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
	}
	b.WriteString(`</Types>`)
	_, err := io.WriteString(out, b.String())
	return err
}

func (w *Workbook) workbook(out io.Writer) error {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sheet := range w.sheets {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(sheet.name), i+1, i+1)
	}
	b.WriteString(`</sheets>`)
	var names strings.Builder
	for i, sheet := range w.sheets {
		if ref := sheet.filterRef(); ref != "" {
			fmt.Fprintf(&names, `<definedName name="_xlnm._FilterDatabase" localSheetId="%d" hidden="1">'%s'!%s</definedName>`,
				i, escape(strings.ReplaceAll(sheet.name, "'", "''")), absoluteRef(ref))
		}
	}
	if names.Len() > 0 {
		b.WriteString(`<definedNames>` + names.String() + `</definedNames>`)
	}
	b.WriteString(`</workbook>`)
	_, err := io.WriteString(out, b.String())
	return err
}

func (w *Workbook) workbookRels(out io.Writer) error {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range w.sheets {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
	}
	fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(w.sheets)+1)
	b.WriteString(`</Relationships>`)
	_, err := io.WriteString(out, b.String())
	return err
}

func (s *Sheet) columnCount() int {
	n := 0
	for _, row := range s.rows {
		if len(row) > n {
			n = len(row)
		}
	}
	return n
}

// filterRef is the table range from the header row to the last row, or "" without a header.
func (s *Sheet) filterRef() string {
	cols := s.columnCount()
	if s.headerRow == 0 || cols == 0 {
		return ""
	}
	last := s.headerRow
	for r := s.headerRow; r < len(s.rows) && len(s.rows[r]) > 0; r++ {
		last = r + 1
	}
	return fmt.Sprintf("A%d:%s%d", s.headerRow, columnName(cols), last)
}

func (s *Sheet) write(out io.Writer) error {
	var b strings.Builder
	b.WriteString(xmlHeader)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)

	cols := s.columnCount()
	if cols > 0 && len(s.rows) > 0 {
		fmt.Fprintf(&b, `<dimension ref="A1:%s%d"/>`, columnName(cols), len(s.rows))
	}
	if s.headerRow > 0 {
		fmt.Fprintf(&b, `<sheetViews><sheetView workbookViewId="0"><pane ySplit="%d" topLeftCell="A%d" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`,
			s.headerRow, s.headerRow+1)
	} else {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"/></sheetViews>`)
	}
	b.WriteString(`<sheetFormatPr defaultRowHeight="15"/>`)
	if len(s.widths) > 0 {
		b.WriteString(`<cols>`)
		for i, width := range s.widths {
			if width <= 0 {
				continue
			}
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%s" customWidth="1"/>`, i+1, i+1, strconv.FormatFloat(width, 'f', -1, 64))
		}
		b.WriteString(`</cols>`)
	}

	b.WriteString(`<sheetData>`)
	for r, row := range s.rows {
		if len(row) == 0 {
			continue
		}
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cell := range row {
			writeCell(&b, fmt.Sprintf("%s%d", columnName(c+1), r+1), cell)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)

	if ref := s.filterRef(); ref != "" {
		fmt.Fprintf(&b, `<autoFilter ref="%s"/>`, ref)
	}
	b.WriteString(`</worksheet>`)
	_, err := io.WriteString(out, b.String())
	return err
}

func writeCell(b *strings.Builder, ref string, cell Cell) {
	style := ""
	if cell.style != StyleDefault {
		style = fmt.Sprintf(` s="%d"`, cell.style)
	}
	switch {
	case cell.numeric && !math.IsNaN(cell.number) && !math.IsInf(cell.number, 0):
		fmt.Fprintf(b, `<c r="%s"%s><v>%s</v></c>`, ref, style, strconv.FormatFloat(cell.number, 'g', -1, 64))
	case cell.text != "":
		fmt.Fprintf(b, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(cell.text))
	case style != "":
		fmt.Fprintf(b, `<c r="%s"%s/>`, ref, style)
	}
}

// columnName converts a 1-based column index to its letters (1 → A, 27 → AA).
func columnName(n int) string {
	name := ""
	for n > 0 {
		n--
		name = string(rune('A'+n%26)) + name
		n /= 26
	}
	return name
}

func absoluteRef(ref string) string {
	parts := strings.Split(ref, ":")
	for i, part := range parts {
		split := strings.IndexAny(part, "0123456789")
		parts[i] = "$" + part[:split] + "$" + part[split:]
	}
	return strings.Join(parts, ":")
}

// escape makes s safe inside XML text and attributes, dropping characters XML 1.0 forbids.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(r)
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF || (r >= 0xD800 && r <= 0xDFFF):
			// not representable in XML 1.0
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sheetName(name string, index int) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, "'")
	if runes := []rune(name); len(runes) > 31 {
		name = string(runes[:31])
	}
	if name == "" {
		name = fmt.Sprintf("Sheet%d", index)
	}
	return name
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

// styles defines cellXfs in the same order as the Style constants.
const styles = xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="2">` +
	`<numFmt numFmtId="164" formatCode="#,##0.00;[Red]-#,##0.00"/>` +
	`<numFmt numFmtId="165" formatCode="0.0%"/>` +
	`</numFmts>` +
	`<fonts count="3">` +
	`<font><sz val="11"/><name val="Calibri"/><family val="2"/></font>` +
	`<font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font>` +
	`<font><b/><sz val="14"/><name val="Calibri"/><family val="2"/></font>` +
	`</fonts>` +
	`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="6">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="3" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`
