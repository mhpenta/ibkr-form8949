package internal

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/types"
)

func ParseCSVFile(filePath string) ([]types.Transaction, int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("open worksheet: %w", err)
	}
	defer f.Close()
	return ParseCSVReader(f)
}

// ParseCSVReader rejects malformed transactions instead of returning partial totals.
// Part/Box values apply until the next section; headers can repeat or reorder fields.
func ParseCSVReader(input io.Reader) ([]types.Transaction, int, error) {
	reader := csv.NewReader(input)
	reader.FieldsPerRecord = -1
	var transactions []types.Transaction
	var header map[string]int
	year, part, boxTerm := 0, 0, 0
	box := ""
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, year, fmt.Errorf("read worksheet CSV: %w", err)
		}
		line, _ := reader.FieldPos(0)
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		row[0] = strings.TrimPrefix(row[0], "\ufeff")
		fail := func(err error) ([]types.Transaction, int, error) {
			return nil, year, fmt.Errorf("line %d: %w", line, err)
		}
		switch row[0] {
		case "Title":
			if len(row) >= 3 && strings.Contains(row[1], "Form 8949") {
				parsed, err := strconv.Atoi(row[2])
				if err != nil || parsed < 2000 || parsed > 2100 {
					return fail(fmt.Errorf("invalid tax year %q", row[2]))
				}
				if year != 0 && year != parsed {
					return fail(fmt.Errorf("conflicting worksheet tax years"))
				}
				year = parsed
			}
		case "Part":
			if len(row) < 2 {
				return fail(fmt.Errorf("missing Part value"))
			}
			switch strings.ToUpper(row[1]) {
			case "I", "1":
				part = 1
			case "II", "2":
				part = 2
			default:
				return fail(fmt.Errorf("unknown Part %q", row[1]))
			}
			box, boxTerm = "", 0
		case "Box":
			if len(row) < 2 {
				return fail(fmt.Errorf("missing Box value"))
			}
			box = strings.ToUpper(row[1])
			switch box {
			case "A", "B", "C":
				boxTerm = 1
			case "D", "E", "F":
				boxTerm = 2
			default:
				return fail(fmt.Errorf("unknown Box %q", box))
			}
			if part != 0 && part != boxTerm {
				return fail(fmt.Errorf("Box %s conflicts with Part", box))
			}
		case "Header":
			header = make(map[string]int)
			for i, field := range row {
				key := strings.ToLower(field)
				if key != "" {
					if _, exists := header[key]; exists {
						return fail(fmt.Errorf("duplicate column %q", field))
					}
					header[key] = i
				}
			}
			for _, name := range worksheetColumns {
				if _, ok := header[name]; !ok {
					return fail(fmt.Errorf("missing column %q", name))
				}
			}
		case "Data":
			if header == nil {
				return fail(fmt.Errorf("transaction before header"))
			}
			fields := []string{"Data"}
			for _, name := range worksheetColumns {
				index := header[name]
				if index >= len(row) {
					return fail(fmt.Errorf("missing value for column %q", name))
				}
				fields = append(fields, row[index])
			}
			tx, err := parseTransaction(fields)
			if err != nil {
				return fail(err)
			}
			term := part
			if boxTerm != 0 {
				term = boxTerm
			}
			if term != 0 {
				tx.IsLongTerm, tx.TermUnknown, tx.TermSource = term == 2, false, "worksheet"
			}
			tx.Box = box
			transactions = append(transactions, tx)
		}
	}
	if header == nil {
		return nil, year, fmt.Errorf("header row not found in CSV")
	}
	if len(transactions) == 0 {
		return nil, year, fmt.Errorf("no transaction rows found in worksheet")
	}
	return transactions, year, nil
}

var worksheetColumns = []string{"description", "date acquired", "date sold", "proceeds", "cost basis", "code(s)", "adjustment", "gain or (loss)"}

// Retained for the focused tokenizer tests; production parsing propagates CSV errors.
func parseCSVLine(line string) []string {
	row, _ := csv.NewReader(strings.NewReader(line)).Read()
	return row
}

// Clamp a leap-day anniversary to February's last day rather than allowing
// time.AddDate to normalize it into March. Compare written calendar dates.
func afterAnniversary(acquired, sold time.Time) bool {
	year, month, day := acquired.Date()
	lastDay := time.Date(year+1, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > lastDay {
		day = lastDay
	}
	anniversary := time.Date(year+1, month, day, 0, 0, 0, 0, time.UTC)
	return time.Date(sold.Year(), sold.Month(), sold.Day(), 0, 0, 0, 0, time.UTC).After(anniversary)
}

func parseTransaction(row []string) (types.Transaction, error) {
	if len(row) < 9 {
		return types.Transaction{}, fmt.Errorf("expected at least 9 fields, got %d", len(row))
	}
	for i := range row {
		row[i] = strings.TrimSpace(row[i])
	}
	if row[1] == "" {
		return types.Transaction{}, fmt.Errorf("missing description")
	}

	hasVariousLot := strings.EqualFold(row[2], "VARIOUS")

	// Parse dates
	dateAcquired, err := parseDate(row[2])
	if err != nil {
		return types.Transaction{}, fmt.Errorf("invalid acquisition date: %w", err)
	}

	dateSold, err := parseDate(row[3])
	if err != nil {
		return types.Transaction{}, fmt.Errorf("invalid sale date: %w", err)
	}
	if dateSold.IsZero() {
		return types.Transaction{}, fmt.Errorf("sale date cannot be VARIOUS")
	}

	// Parse numeric values
	proceeds, err := parseAmount(row[4])
	if err != nil {
		return types.Transaction{}, fmt.Errorf("invalid proceeds: %w", err)
	}

	costBasis, err := parseAmount(row[5])
	if err != nil {
		return types.Transaction{}, fmt.Errorf("invalid cost basis: %w", err)
	}

	var adjustment float64
	if row[7] != "" {
		adjustment, err = parseAmount(row[7])
		if err != nil {
			return types.Transaction{}, fmt.Errorf("invalid adjustment: %w", err)
		}
	}

	gainOrLoss, err := parseAmount(row[8])
	if err != nil {
		return types.Transaction{}, fmt.Errorf("invalid gain/loss: %w", err)
	}

	// Check if this is a wash sale
	isWashSale := strings.Contains(row[6], "W")

	// Worksheet sections override this fallback in ParseCSVReader.
	isLongTerm := !hasVariousLot && afterAnniversary(dateAcquired, dateSold)
	// Determine which calendar quarter the sale occurred in
	quarter := (dateSold.Month()-1)/3 + 1

	return types.Transaction{
		Description:   row[1],
		DateAcquired:  dateAcquired,
		DateSold:      dateSold,
		Proceeds:      proceeds,
		CostBasis:     costBasis,
		Codes:         row[6],
		Adjustment:    adjustment,
		GainOrLoss:    gainOrLoss,
		IsWashSale:    isWashSale,
		HasVariousLot: hasVariousLot,
		IsLongTerm:    isLongTerm,
		TermUnknown:   hasVariousLot,
		TermSource:    "dates",
		Quarter:       int(quarter),
	}, nil
}

func parseDate(dateStr string) (time.Time, error) {
	// Handle "VARIOUS" dates (common in Form 8949 for lot aggregation)
	dateStr = strings.TrimSpace(dateStr)
	if strings.EqualFold(dateStr, "VARIOUS") {
		return time.Time{}, nil
	}

	// Try different date formats
	// For MM/DD/YYYY format
	if t, err := time.Parse("01/02/2006", dateStr); err == nil {
		return t, nil
	}

	// For MM-DD-YYYY format
	if t, err := time.Parse("01-02-2006", dateStr); err == nil {
		return t, nil
	}

	// For YYYY-MM-DD format
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		return t, nil
	}

	// For Interactive Brokers typical MM/DD/YYYY format
	return time.Parse("01/02/2006", dateStr)
}

func parseAmount(amountStr string) (float64, error) {
	// Remove currency symbols, commas, and parentheses for negative numbers
	clean := strings.ReplaceAll(strings.TrimSpace(amountStr), "$", "")
	clean = strings.ReplaceAll(clean, ",", "")

	// Check if the number is in parentheses (indicating negative)
	isNegative := strings.HasPrefix(clean, "(") && strings.HasSuffix(clean, ")")
	if isNegative {
		clean = strings.TrimPrefix(clean, "(")
		clean = strings.TrimSuffix(clean, ")")
	}

	// Parse the clean string
	amount, err := strconv.ParseFloat(strings.TrimSpace(clean), 64)
	if err != nil {
		return 0, err
	}

	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("amount must be finite")
	}

	if isNegative {
		amount = -amount
	}

	return amount, nil
}
