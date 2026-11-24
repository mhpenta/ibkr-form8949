package activity

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/washsale"
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
	StatementLocation               *time.Location
	Today                           time.Time
	AccountID                       string
	BaseCurrency                    string
	// OpeningPositions holds long quantities held before the first trade, keyed
	// by inventory, where the broker's position summary reconciles all trades.
	OpeningPositions map[string]float64
	// Coverage lists the date ranges the statements cover. Empty means the single
	// range PeriodStart through PeriodEnd, when those are known.
	Coverage []DateRange
	// Sources describes each statement file the report was read from.
	Sources []StatementSource
	// MarkToMarketPLUnavailable is set when combined statements overlap or not all
	// have a Mark-to-Market summary, so per-period P/L cannot be added up. The
	// summary rows then carry quantities only.
	MarkToMarketPLUnavailable bool
}

// StatementSource is one statement file read into a report.
type StatementSource struct {
	Name       string // file name, without directories
	Period     string
	Start, End time.Time
	Generated  time.Time
	Trades     int // order or execution trades in the file
	TradesUsed int // trades kept after combining overlapping statements
}

// Trade represents a single trade record from an IBKR activity report
type Trade struct {
	Source            string // statement file name; set when statements are loaded from files
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
	HasShortLossLot   bool // A closed-lot row for this buy-to-cover reported a loss
	// ClosedLots are the broker's closed-lot rows for this sale, when exported.
	ClosedLots []ClosedLot
	// closedLotsInvalid marks closed-lot rows that could not be read or assigned.
	closedLotsInvalid bool
}

// ClosedLot is one broker closed-lot row: shares a sale took from one tax lot.
type ClosedLot struct {
	Acquired   time.Time // Calendar day the lot was acquired
	Quantity   float64   // Shares taken from the lot, always positive
	RealizedPL float64
}

// IsLossSale reports a sale that can start a wash-sale window: a net loss, an
// LD-coded sale, or a net profitable sale whose closed-lot detail shows a losing lot.
func (t Trade) IsLossSale() bool {
	return t.IsSale && (t.RealizedPL < 0 || t.IsWashSale || t.HasLossLot)
}

// IsShortCoverLoss reports a buy-to-cover that closed a short position at a loss
// or carries a wash-sale (LD) adjustment, which may trigger a short-sale wash window
// under IRC §1091(e) and requires separate review of delivery and lot details.
func (t Trade) IsShortCoverLoss() bool {
	return t.IsBuy && (t.RealizedPL < 0 || t.IsWashSale || t.HasShortLossLot)
}

// AsOfInStatementZone converts a wall-clock instant into the statement's timezone
// when known, so calendar-day comparisons match the statement's trade dates
// regardless of the machine's local timezone.
func (r *ActivityReport) AsOfInStatementZone(now time.Time) time.Time {
	if r != nil && r.StatementLocation != nil {
		return now.In(r.StatementLocation)
	}
	return now
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
	report, err := parseActivityFile(filePath)
	if err == nil {
		for _, warning := range report.Warnings {
			slog.Warn(warning)
		}
	}
	return report, err
}

func parseActivityFile(filePath string) (*ActivityReport, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	report, err := ParseActivityReportReader(file)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(filePath)
	report.Sources[0].Name = name
	for i := range report.Trades {
		report.Trades[i].Source = name
	}
	for symbol, trades := range report.TradesBySymbol {
		for i := range trades {
			trades[i].Source = name
		}
		report.TradesBySymbol[symbol] = trades
	}
	return report, nil
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
		OpeningPositions:                make(map[string]float64),
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
			report.StatementDate, report.StatementLocation, err = parseStatementDate(record[3])
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
	// Closed-lot rows follow the closing trade they belong to. Attach each lot
	// directly to its trade so a losing lot does not leak onto unrelated same-day trades.
	lastSale := -1
	lastCover := -1
	for i, record := range records {
		if len(record) < 2 || record[0] != "Trades" {
			continue
		}
		if record[1] == "Header" {
			lastSale = -1
			lastCover = -1
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
				sale := &candidates[lastSale].trade
				pl, plErr := parseNumber(getField(record, fields, "Realized P/L"))
				if plErr == nil && pl < 0 {
					sale.HasLossLot = true
				}
				acquired, dateErr := parseTradeDateTime(getField(record, fields, "Date/Time"))
				quantity, quantityErr := parseNumber(getField(record, fields, "Quantity"))
				if plErr != nil || dateErr != nil || quantityErr != nil || quantity == 0 {
					sale.closedLotsInvalid = true
				} else {
					sale.ClosedLots = append(sale.ClosedLots, ClosedLot{Acquired: washsale.Day(acquired), Quantity: math.Abs(quantity), RealizedPL: pl})
				}
			} else if lastCover >= 0 && strings.EqualFold(getField(record, fields, "Symbol"), candidates[lastCover].trade.Symbol) {
				pl, plErr := parseNumber(getField(record, fields, "Realized P/L"))
				if plErr == nil && pl < 0 {
					candidates[lastCover].trade.HasShortLossLot = true
				}
			}
			continue
		}
		if strings.HasPrefix(level, "Total") || strings.HasPrefix(level, "SubTotal") || level == "Lot" {
			lastSale = -1
			lastCover = -1
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
		lastCover = -1
		if isClosingSale(trade) {
			lastSale = len(candidates) - 1
		} else if isShortCover(trade) {
			lastCover = len(candidates) - 1
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
	// When order rows replace executions, closed lots that followed the
	// executions move to the order. Several same-day orders make that ambiguous.
	droppedLots := make(map[string][]ClosedLot)
	droppedInvalid := make(map[string]bool)
	droppedLossLot := make(map[string]bool)
	keptSales := make(map[string]int)
	keptHasLoss := make(map[string]bool)
	droppedShortLossLot := make(map[string]bool)
	keptCovers := make(map[string]int)
	keptHasShortLoss := make(map[string]bool)
	for _, c := range candidates {
		dropped := c.level == "Trade" && len(levels[tradeInventoryKey(c.trade)]["Order"]) > 0
		if isClosingSale(c.trade) {
			key := lossLotKey(c.trade)
			if dropped {
				droppedLots[key] = append(droppedLots[key], c.trade.ClosedLots...)
				droppedInvalid[key] = droppedInvalid[key] || c.trade.closedLotsInvalid
				droppedLossLot[key] = droppedLossLot[key] || c.trade.HasLossLot
				continue
			}
			keptSales[key]++
			if c.trade.IsLossSale() {
				keptHasLoss[key] = true
			}
		} else if isShortCover(c.trade) {
			key := lossLotKey(c.trade)
			if dropped {
				droppedShortLossLot[key] = droppedShortLossLot[key] || c.trade.HasShortLossLot
				continue
			}
			keptCovers[key]++
			if c.trade.IsShortCoverLoss() {
				keptHasShortLoss[key] = true
			}
		}
	}
	droppedLossAssigned := make(map[string]bool)
	droppedShortLossAssigned := make(map[string]bool)
	for _, c := range candidates {
		if c.level == "Trade" && len(levels[tradeInventoryKey(c.trade)]["Order"]) > 0 {
			continue
		}
		if isClosingSale(c.trade) {
			key := lossLotKey(c.trade)
			if len(droppedLots[key]) > 0 || droppedInvalid[key] {
				if keptSales[key] == 1 && len(c.trade.ClosedLots) == 0 {
					c.trade.ClosedLots = droppedLots[key]
					c.trade.closedLotsInvalid = c.trade.closedLotsInvalid || droppedInvalid[key]
				} else {
					c.trade.closedLotsInvalid = true
				}
			}
			if droppedLossLot[key] {
				if keptSales[key] == 1 {
					c.trade.HasLossLot = true
				} else if !keptHasLoss[key] && !droppedLossAssigned[key] {
					// Several net-profitable same-day orders share dropped execution lots;
					// mark one so the day's loss-sale window is not silently lost.
					c.trade.HasLossLot = true
					droppedLossAssigned[key] = true
				}
			}
		} else if isShortCover(c.trade) {
			key := lossLotKey(c.trade)
			if droppedShortLossLot[key] {
				if keptCovers[key] == 1 {
					c.trade.HasShortLossLot = true
				} else if !keptHasShortLoss[key] && !droppedShortLossAssigned[key] {
					c.trade.HasShortLossLot = true
					droppedShortLossAssigned[key] = true
				}
			}
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
	if !report.PeriodStart.IsZero() && !report.PeriodEnd.IsZero() {
		report.Coverage = []DateRange{{report.PeriodStart, report.PeriodEnd}}
	}
	report.Sources = []StatementSource{{
		Period: report.Period, Start: report.PeriodStart, End: report.PeriodEnd, Generated: report.StatementDate,
		Trades: len(report.Trades), TradesUsed: len(report.Trades),
	}}

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

// isClosingSale reports a sale that can close a long lot (not a pure short opening).
func isClosingSale(t Trade) bool {
	return t.IsSale && (!t.HasCode("O") || t.HasCode("C"))
}

// isShortCover reports a buy that closes a short position.
func isShortCover(t Trade) bool {
	return t.IsBuy && t.HasCode("C")
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

type statementZoneSpec struct {
	offset int    // fixed UTC offset in seconds for the abbreviation
	iana   string // DST-aware IANA timezone name for converting wall-clock time.Now()
}

var statementZones = map[string]statementZoneSpec{
	"UTC":  {0, "UTC"},
	"GMT":  {0, "UTC"},
	"EST":  {-5 * 3600, "America/New_York"},
	"EDT":  {-4 * 3600, "America/New_York"},
	"CST":  {-6 * 3600, "America/Chicago"},
	"CDT":  {-5 * 3600, "America/Chicago"},
	"MST":  {-7 * 3600, "America/Denver"},
	"MDT":  {-6 * 3600, "America/Denver"},
	"PST":  {-8 * 3600, "America/Los_Angeles"},
	"PDT":  {-7 * 3600, "America/Los_Angeles"},
	"AKST": {-9 * 3600, "America/Anchorage"},
	"AKDT": {-8 * 3600, "America/Anchorage"},
	"HST":  {-10 * 3600, "Pacific/Honolulu"},
	"WET":  {0, "Europe/London"},
	"WEST": {1 * 3600, "Europe/London"},
	"BST":  {1 * 3600, "Europe/London"},
	"CET":  {1 * 3600, "Europe/Berlin"},
	"CEST": {2 * 3600, "Europe/Berlin"},
	"EET":  {2 * 3600, "Europe/Athens"},
	"EEST": {3 * 3600, "Europe/Athens"},
	"JST":  {9 * 3600, "Asia/Tokyo"},
	"HKT":  {8 * 3600, "Asia/Hong_Kong"},
	"SGT":  {8 * 3600, "Asia/Singapore"},
	"AEST": {10 * 3600, "Australia/Sydney"},
	"AEDT": {11 * 3600, "Australia/Sydney"},
	"NZST": {12 * 3600, "Pacific/Auckland"},
	"NZDT": {13 * 3600, "Pacific/Auckland"},
}

// parseStatementDate parses IBKR's WhenGenerated timestamp, resolving known
// timezone abbreviations to their actual UTC offsets rather than Go's zero-offset
// placeholder, and returns a location for converting current wall-clock times.
func parseStatementDate(value string) (time.Time, *time.Location, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02, 15:04:05 -0700", "2006-01-02, 15:04:05 Z07:00"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, t.Location(), nil
		}
	}
	t, err := time.Parse("2006-01-02, 15:04:05 MST", value)
	if err != nil {
		return time.Time{}, nil, err
	}
	zoneName, offset := t.Zone()
	if spec, ok := statementZones[zoneName]; ok {
		fixed := time.FixedZone(zoneName, spec.offset)
		parsed := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), fixed)
		loc, loadErr := time.LoadLocation(spec.iana)
		if loadErr != nil {
			loc = fixed
		}
		return parsed, loc, nil
	}
	if offset != 0 {
		return t, t.Location(), nil
	}
	return t, nil, nil
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
	fields := make(map[string]float64)
	for _, record := range records {
		if len(record) < 4 || record[0] != "Change in NAV" || record[1] != "Data" {
			continue
		}
		fields[strings.TrimSpace(record[2])] = parseFloat(record[3])
	}
	return changeInNAVFromFields(fields)
}

// changeInNAVFromFields fills the named NAV lines from IBKR's field names.
func changeInNAVFromFields(fields map[string]float64) ChangeInNAV {
	nav := ChangeInNAV{Fields: fields}
	for field, value := range fields {
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
