package activity

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ActivityReport represents a parsed IBKR Activity Report
type ActivityReport struct {
	Trades                          []Trade
	TradesBySymbol                  map[string][]Trade
	ChangeInNAV                     ChangeInNAV
	MarkToMarketPerformance         []MarkToMarketPerformance
	MarkToMarketPerformanceBySymbol map[string][]MarkToMarketPerformance
	Period                          string
	PeriodStart                     time.Time
	PeriodEnd                       time.Time
	Warnings                        []string
	StatementDate                   time.Time
	Today                           time.Time
	AccountID                       string
	BaseCurrency                    string
}

// Trade represents a single trade record from an IBKR activity report
type Trade struct {
	Account           string
	DateOnly          bool
	PositionKnownFlat bool // Confirmed by reconciled broker position quantities
	AssetCategory     string
	Currency          string
	Symbol            string
	DateTime          time.Time
	Quantity          float64
	TradePrice        float64
	ClosePrice        float64
	Proceeds          float64
	CommFee           float64
	Basis             float64
	RealizedPL        float64
	RealizedPLMissing bool
	MTMPL             float64
	Code              string
	IsBuy             bool
	IsSale            bool
	IsOption          bool
	IsWashSale        bool
	HasLossLot        bool // A closed-lot row for this sale reported a loss
}

// IsLossSale reports a sale that can start a wash-sale window: a net loss, an
// LD-coded sale, or a net profitable sale whose closed-lot detail shows a losing lot.
func (t Trade) IsLossSale() bool {
	return t.IsSale && (t.RealizedPL < 0 || t.IsWashSale || t.HasLossLot)
}

// HasCode checks for a complete IBKR trade code in the semicolon-separated list.
func (t Trade) HasCode(want string) bool {
	for _, code := range strings.Split(t.Code, ";") {
		if strings.TrimSpace(code) == want {
			return true
		}
	}
	return false
}

// ChangeInNAV represents IBKR's Change in NAV account-level summary.
type ChangeInNAV struct {
	Fields                   map[string]float64
	StartingValue            float64
	MarkToMarket             float64
	DepositsWithdrawals      float64
	PositionTransfers        float64
	Dividends                float64
	WithholdingTax           float64
	ChangeInDividendAccruals float64
	Interest                 float64
	ChangeInInterestAccruals float64
	OtherFees                float64
	Commissions              float64
	TransactionFees          float64
	OtherFXTranslations      float64
	EndingValue              float64
}

func (nav ChangeInNAV) HasData() bool {
	return len(nav.Fields) > 0
}

func (nav ChangeInNAV) NetNAVChange() float64 {
	return nav.EndingValue - nav.StartingValue
}

func (nav ChangeInNAV) ExternalFlows() float64 {
	return nav.DepositsWithdrawals + nav.PositionTransfers
}

func (nav ChangeInNAV) GainBeforeExternalFlows() float64 {
	return nav.NetNAVChange() - nav.ExternalFlows()
}

// MarkToMarketPerformance represents a row from IBKR's Mark-to-Market Performance Summary.
// Amounts in this section are reported in the account base currency.
type MarkToMarketPerformance struct {
	AssetCategory          string
	Symbol                 string
	PriorQuantity          float64
	CurrentQuantity        float64
	PriorQuantityMissing   bool
	CurrentQuantityMissing bool
	PriorPrice             float64
	CurrentPrice           float64
	PositionPL             float64
	TransactionPL          float64
	CommissionPL           float64
	OtherPL                float64
	TotalPL                float64
	Code                   string
}

// ParseActivityReport parses an IBKR activity report CSV file
func ParseActivityReport(filePath string) (*ActivityReport, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	report, err := ParseActivityReportReader(file)
	if err == nil {
		for _, warning := range report.Warnings {
			slog.Warn(warning)
		}
	}
	return report, err
}

// ParseActivityReportReader parses an IBKR activity report from a reader
func ParseActivityReportReader(r io.Reader) (*ActivityReport, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // Allow variable number of fields

	var records [][]string
	var lines []int
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read activity CSV: %w", err)
		}
		line, _ := reader.FieldPos(0)
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		row[0] = strings.TrimPrefix(row[0], "\ufeff")
		records = append(records, row)
		lines = append(lines, line)
	}

	report := &ActivityReport{
		TradesBySymbol:                  make(map[string][]Trade),
		MarkToMarketPerformanceBySymbol: make(map[string][]MarkToMarketPerformance),
	}

	// Process header information (period, statement date, account ID)
	for _, record := range records {
		if len(record) < 4 {
			continue
		}

		if record[0] == "Statement" && record[1] == "Data" && record[2] == "Period" {
			report.Period = record[3]
			report.PeriodStart, report.PeriodEnd = parsePeriod(record[3])
		}

		if record[0] == "Statement" && record[1] == "Data" && record[2] == "WhenGenerated" {
			var err error
			report.StatementDate, err = time.Parse("2006-01-02, 15:04:05 MST", record[3])
			if err != nil {
				return nil, fmt.Errorf("invalid statement generation date: %w", err)
			}
		}

		if record[0] == "Account Information" && record[1] == "Data" && record[2] == "Account" {
			report.AccountID = record[3]
		}

		if record[0] == "Account Information" && record[1] == "Data" && record[2] == "Base Currency" {
			report.BaseCurrency = record[3]
		}
	}

	if err := validateSummaryNumbers(records, lines); err != nil {
		return nil, err
	}
	report.ChangeInNAV = parseChangeInNAV(records)
	report.MarkToMarketPerformance = parseMarkToMarketPerformance(records)
	for _, performance := range report.MarkToMarketPerformance {
		if performance.Symbol == "" {
			continue
		}
		report.MarkToMarketPerformanceBySymbol[performance.Symbol] = append(report.MarkToMarketPerformanceBySymbol[performance.Symbol], performance)
	}

	// Resolve each section against its own header and retain one detail level.
	var header []string
	type candidate struct {
		trade Trade
		level string
	}
	var candidates []candidate
	levels := make(map[string]map[string][]Trade)
	// Closed-lot rows follow the sale they close. A losing lot inside a net
	// profitable sale still starts a window, so remember its sale's key and day.
	// The window depends only on the sale day, so same-day sales share the mark.
	lossLotSales := make(map[string]bool)
	lastSale := -1
	for i, record := range records {
		if len(record) < 2 || record[0] != "Trades" {
			continue
		}
		if record[1] == "Header" {
			lastSale = -1
			header = record
			if _, err := fieldIndexes(header); err != nil {
				return nil, fmt.Errorf("line %d: %w", lines[i], err)
			}
			continue
		}
		if record[1] != "Data" {
			continue
		}
		if header == nil {
			return nil, fmt.Errorf("line %d: trade data before header", lines[i])
		}
		fields, _ := fieldIndexes(header)
		level := getField(record, fields, "DataDiscriminator")
		if level == "ClosedLot" {
			if lastSale >= 0 && strings.EqualFold(getField(record, fields, "Symbol"), candidates[lastSale].trade.Symbol) {
				if pl, err := parseNumber(getField(record, fields, "Realized P/L")); err == nil && pl < 0 {
					lossLotSales[lossLotKey(candidates[lastSale].trade)] = true
				}
			}
			continue
		}
		if strings.HasPrefix(level, "Total") || strings.HasPrefix(level, "SubTotal") || level == "Lot" {
			lastSale = -1
			continue
		}
		if level != "Order" && level != "Trade" {
			return nil, fmt.Errorf("line %d: unsupported trade detail level %q", lines[i], level)
		}
		trade, err := parseTrade(record, header)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lines[i], err)
		}
		if trade.Account == "" {
			trade.Account = report.AccountID
		}
		key := tradeInventoryKey(trade)
		if levels[key] == nil {
			levels[key] = make(map[string][]Trade)
		}
		levels[key][level] = append(levels[key][level], trade)
		candidates = append(candidates, candidate{trade, level})
		lastSale = -1
		if trade.IsSale {
			lastSale = len(candidates) - 1
		}
		if _, ok := fields["Code"]; !ok {
			report.Warnings = append(report.Warnings, "Trade codes are absent; broker wash-sale flags are unavailable")
		}
	}
	keys := make([]string, 0, len(levels))
	for key := range levels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		both := levels[key]
		if len(both["Order"]) > 0 && len(both["Trade"]) > 0 && !equivalentTradeLevels(both["Order"], both["Trade"]) {
			return nil, fmt.Errorf("order and execution detail totals disagree; export one consistent trade detail level")
		}
	}
	for _, c := range candidates {
		if c.level == "Trade" && len(levels[tradeInventoryKey(c.trade)]["Order"]) > 0 {
			continue
		}
		if c.trade.IsSale && lossLotSales[lossLotKey(c.trade)] {
			c.trade.HasLossLot = true
		}
		report.Trades = append(report.Trades, c.trade)
	}
	if header == nil && len(report.MarkToMarketPerformance) == 0 {
		return nil, fmt.Errorf("trades section header not found")
	}
	if len(report.Trades) == 0 {
		report.Warnings = append(report.Warnings, "No order or execution transactions found; trade-based results are empty")
	}
	if report.PeriodStart.IsZero() || report.PeriodEnd.IsZero() {
		report.Warnings = append(report.Warnings, "Statement coverage dates are unavailable; wash-sale window completeness is unknown")
	}
	markKnownFlatPositions(report)
	for _, trade := range report.Trades {
		report.TradesBySymbol[trade.Symbol] = append(report.TradesBySymbol[trade.Symbol], trade)
	}
	sort.Strings(report.Warnings)
	unique := report.Warnings[:0]
	for _, w := range report.Warnings {
		if len(unique) == 0 || unique[len(unique)-1] != w {
			unique = append(unique, w)
		}
	}
	report.Warnings = unique

	if !report.StatementDate.IsZero() {
		report.Today = report.StatementDate
	} else {
		report.Today = time.Now()
	}

	return report, nil
}

// parseTrade parses a single trade record
func parseTrade(record, header []string) (Trade, error) {
	var t Trade
	fields, err := fieldIndexes(header)
	if err != nil {
		return t, err
	}
	for i, name := range header {
		if name != "" && i >= len(record) {
			return t, fmt.Errorf("missing trade value column %q", name)
		}
	}
	for _, name := range []string{"Asset Category", "Currency", "Symbol", "Date/Time", "Quantity", "T. Price", "Proceeds"} {
		if _, ok := fields[name]; !ok {
			return t, fmt.Errorf("missing trade column %q", name)
		}
		if getField(record, fields, name) == "" {
			return t, fmt.Errorf("missing trade value %q", name)
		}
	}
	t.Account = getField(record, fields, "Account")
	t.AssetCategory = getField(record, fields, "Asset Category")
	t.Currency = strings.ToUpper(getField(record, fields, "Currency"))
	t.Symbol = strings.ToUpper(getField(record, fields, "Symbol"))
	date := getField(record, fields, "Date/Time")
	t.DateTime, err = parseTradeDateTime(date)
	if err != nil {
		return t, fmt.Errorf("invalid trade date: %w", err)
	}
	t.DateOnly = len(date) == len(time.DateOnly)
	t.Code = getField(record, fields, "Code")
	t.IsOption = strings.Contains(t.AssetCategory, "Options")
	t.RealizedPLMissing = missingNumber(getField(record, fields, "Realized P/L"))
	if t.AssetCategory != "Forex" {
		for _, name := range []string{"Comm/Fee", "Basis", "Realized P/L"} {
			if _, ok := fields[name]; !ok {
				return t, fmt.Errorf("missing trade column %q", name)
			}
		}
	}
	numeric := []struct {
		name     string
		dst      *float64
		required bool
	}{
		{"Quantity", &t.Quantity, true}, {"T. Price", &t.TradePrice, true}, {"C. Price", &t.ClosePrice, false},
		{"Proceeds", &t.Proceeds, true}, {"Comm/Fee", &t.CommFee, t.AssetCategory != "Forex"},
		{"Basis", &t.Basis, t.AssetCategory != "Forex"}, {"Realized P/L", &t.RealizedPL, t.AssetCategory != "Forex"}, {"MTM P/L", &t.MTMPL, false},
	}
	for _, field := range numeric {
		value := getField(record, fields, field.name)
		if t.AssetCategory == "Forex" {
			prefix := ""
			if field.name == "Comm/Fee" {
				prefix = "Comm in "
			}
			if field.name == "MTM P/L" {
				prefix = "MTM in "
			}
			if prefix != "" {
				for _, name := range header {
					if strings.HasPrefix(name, prefix) {
						value = getField(record, fields, name)
						break
					}
				}
			}
		}
		if field.required && missingNumber(value) {
			return t, fmt.Errorf("missing numeric value %q", field.name)
		}
		*field.dst, err = parseNumber(value)
		if err != nil {
			return t, fmt.Errorf("invalid %s: %w", field.name, err)
		}
	}
	t.IsBuy, t.IsSale = t.Quantity > 0, t.Quantity < 0
	t.IsWashSale = t.HasCode("LD")
	return t, nil
}

func fieldIndexes(header []string) (map[string]int, error) {
	fields := make(map[string]int)
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := fields[name]; ok {
			return nil, fmt.Errorf("duplicate column %q", name)
		}
		fields[name] = i
	}
	return fields, nil
}

func missingNumber(value string) bool { return value == "" || value == "--" }

func parseNumber(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if missingNumber(value) {
		return 0, nil
	}
	value = strings.ReplaceAll(value, ",", "")
	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = "-" + value[1:len(value)-1]
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, fmt.Errorf("number must be finite")
	}
	return n, nil
}

func tradeInventoryKey(t Trade) string {
	return t.Account + "\x00" + t.AssetCategory + "\x00" + t.Currency + "\x00" + t.Symbol
}

func lossLotKey(t Trade) string {
	return tradeInventoryKey(t) + "\x00" + t.DateTime.Format(time.DateOnly)
}

// Compare per-day buy/sell totals so partial execution detail cannot silently
// replace or duplicate a different set of orders. Broker rounding is tolerated.
func equivalentTradeLevels(a, b []Trade) bool {
	totals := func(trades []Trade) map[string][6]float64 {
		out := make(map[string][6]float64)
		for _, t := range trades {
			key := t.DateTime.Format(time.DateOnly) + fmt.Sprint(t.IsBuy)
			v := out[key]
			for i, n := range []float64{t.Quantity, t.Proceeds, t.CommFee, t.Basis, t.RealizedPL, t.MTMPL} {
				v[i] += n
			}
			out[key] = v
		}
		return out
	}
	x, y := totals(a), totals(b)
	if len(x) != len(y) {
		return false
	}
	for key, v := range x {
		w, ok := y[key]
		if !ok {
			return false
		}
		for i := range v {
			tolerance := 0.011
			if i == 0 {
				tolerance = 1e-8
			}
			if math.Abs(v[i]-w[i]) > tolerance {
				return false
			}
		}
	}
	return true
}

func parseTradeDateTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)

	var lastErr error
	for _, layout := range []string{"2006-01-02, 15:04:05", time.DateOnly} {
		dateTime, err := time.Parse(layout, value)
		if err == nil {
			return dateTime, nil
		}
		lastErr = err
	}

	return time.Time{}, lastErr
}

func parseChangeInNAV(records [][]string) ChangeInNAV {
	nav := ChangeInNAV{
		Fields: make(map[string]float64),
	}

	for _, record := range records {
		if len(record) < 4 || record[0] != "Change in NAV" || record[1] != "Data" {
			continue
		}

		field := strings.TrimSpace(record[2])
		value := parseFloat(record[3])
		nav.Fields[field] = value

		switch field {
		case "Starting Value":
			nav.StartingValue = value
		case "Mark-to-Market":
			nav.MarkToMarket = value
		case "Deposits & Withdrawals":
			nav.DepositsWithdrawals = value
		case "Position Transfers":
			nav.PositionTransfers = value
		case "Dividends":
			nav.Dividends = value
		case "Withholding Tax":
			nav.WithholdingTax = value
		case "Change in Dividend Accruals":
			nav.ChangeInDividendAccruals = value
		case "Interest":
			nav.Interest = value
		case "Change in Interest Accruals":
			nav.ChangeInInterestAccruals = value
		case "Other Fees":
			nav.OtherFees = value
		case "Commissions":
			nav.Commissions = value
		case "Transaction Fees":
			nav.TransactionFees = value
		case "Other FX Translations":
			nav.OtherFXTranslations = value
		case "Ending Value":
			nav.EndingValue = value
		}
	}

	return nav
}

func parseMarkToMarketPerformance(records [][]string) []MarkToMarketPerformance {
	var header []string
	var performances []MarkToMarketPerformance

	for _, record := range records {
		if len(record) >= 3 && record[0] == "Mark-to-Market Performance Summary" && record[1] == "Header" {
			header = record
			continue
		}

		if len(record) < 3 || record[0] != "Mark-to-Market Performance Summary" || record[1] != "Data" || header == nil {
			continue
		}

		performances = append(performances, parseMarkToMarketPerformanceRow(record, header))
	}

	return performances
}

func parseMarkToMarketPerformanceRow(record, header []string) MarkToMarketPerformance {
	fieldMap := make(map[string]int)
	for i, field := range header {
		fieldMap[field] = i
	}
	prior := getField(record, fieldMap, "Prior Quantity")
	current := getField(record, fieldMap, "Current Quantity")

	return MarkToMarketPerformance{
		AssetCategory:          getField(record, fieldMap, "Asset Category"),
		Symbol:                 strings.ToUpper(getField(record, fieldMap, "Symbol")),
		PriorQuantity:          parseFloat(prior),
		CurrentQuantity:        parseFloat(current),
		PriorQuantityMissing:   missingNumber(prior),
		CurrentQuantityMissing: missingNumber(current),
		PriorPrice:             parseFloat(getField(record, fieldMap, "Prior Price")),
		CurrentPrice:           parseFloat(getField(record, fieldMap, "Current Price")),
		PositionPL:             parseFloat(getField(record, fieldMap, "Mark-to-Market P/L Position")),
		TransactionPL:          parseFloat(getField(record, fieldMap, "Mark-to-Market P/L Transaction")),
		CommissionPL:           parseFloat(getField(record, fieldMap, "Mark-to-Market P/L Commissions")),
		OtherPL:                parseFloat(getField(record, fieldMap, "Mark-to-Market P/L Other")),
		TotalPL:                parseFloat(getField(record, fieldMap, "Mark-to-Market P/L Total")),
		Code:                   getField(record, fieldMap, "Code"),
	}
}

func getField(record []string, fieldMap map[string]int, field string) string {
	index, ok := fieldMap[field]
	if !ok || index >= len(record) {
		return ""
	}
	return record[index]
}

// Summary values are validated before these conversion helpers run.
func parseFloat(value string) float64 { n, _ := parseNumber(value); return n }
