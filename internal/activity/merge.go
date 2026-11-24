package activity

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

// MergeOptions changes how statements are combined.
type MergeOptions struct {
	// AllowMultipleAccounts combines statements from different accounts.
	// Without it, they are refused with an AccountMismatchError.
	AllowMultipleAccounts bool
}

// AccountMismatchError reports statements from different accounts that were not
// combined because combining accounts was not allowed.
type AccountMismatchError struct {
	Accounts []string            // in the order first seen
	Files    map[string][]string // account → statement file names
}

func (e *AccountMismatchError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "These statements come from %d different accounts, so they were not combined:\n", len(e.Accounts))
	for _, account := range e.Accounts {
		fmt.Fprintf(&b, "  Account %s: %s\n", account, strings.Join(e.Files[account], ", "))
	}
	b.WriteString("\nTo look at one account, run again with only that account's statements.\n")
	b.WriteString("To combine the accounts on purpose, add --allow-multiple-accounts. Positions are then added\n")
	b.WriteString("together across accounts, but wash-sale replacements are still matched within each account only.")
	return b.String()
}

// LoadActivityReports reads one or more activity statement files. Several files
// are combined with MergeReports, and every warning is logged.
func LoadActivityReports(paths []string, opts MergeOptions) (*ActivityReport, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no activity statement files supplied")
	}
	if len(paths) == 1 {
		return ParseActivityReport(paths[0])
	}
	reports := make([]*ActivityReport, len(paths))
	for i, path := range paths {
		report, err := parseActivityFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		reports[i] = report
	}
	merged, err := MergeReports(reports, opts)
	if err != nil {
		return nil, err
	}
	for _, warning := range merged.Warnings {
		slog.Warn(warning)
	}
	return merged, nil
}

// MergeReports combines statements into one report on a best-effort basis:
//
//   - Every statement needs a statement period, and all must share a base currency.
//   - Statements from different accounts are refused unless opts allows them.
//     When allowed, every rule below applies within each account separately.
//   - Where periods overlap, each day's trades come from one statement only: the
//     most recently generated one covering that day (the later-starting one on a tie).
//     Overlaps are warned about, and so are statements that disagree on those days.
//   - Days no statement covers are gaps: they are warned about, and wash-sale
//     windows that touch them count as not covered. With several accounts, only
//     days every account's statements cover count as covered.
//   - Position quantities come from each account's earliest statement (opening)
//     and latest one (ending). Mark-to-Market P/L is added up only when no
//     statements overlap, and Change in NAV only for a single account whose
//     statements also leave no gaps.
//
// The reports are not modified.
func MergeReports(reports []*ActivityReport, opts MergeOptions) (*ActivityReport, error) {
	if len(reports) == 0 {
		return nil, fmt.Errorf("no statements to combine")
	}
	if len(reports) == 1 {
		return reports[0], nil
	}
	order := make([]int, len(reports))
	baseCurrency := ""
	for i, r := range reports {
		order[i] = i
		if r.PeriodStart.IsZero() || r.PeriodEnd.IsZero() {
			return nil, fmt.Errorf("%s has no statement period, so it cannot be combined with other statements", sourceName(r, i))
		}
		if r.BaseCurrency != "" {
			if baseCurrency != "" && !strings.EqualFold(baseCurrency, r.BaseCurrency) {
				return nil, fmt.Errorf("statements use different base currencies (%s and %s) and cannot be combined", baseCurrency, r.BaseCurrency)
			}
			baseCurrency = r.BaseCurrency
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := reports[order[a]], reports[order[b]]
		if !ra.PeriodStart.Equal(rb.PeriodStart) {
			return ra.PeriodStart.Before(rb.PeriodStart)
		}
		return ra.PeriodEnd.Before(rb.PeriodEnd)
	})
	sorted := make([]*ActivityReport, len(order))
	names := make([]string, len(order))
	for i, index := range order {
		sorted[i] = reports[index]
		names[i] = sourceName(reports[index], index)
	}

	// Group statements by account, in input order for the error message.
	mismatch := &AccountMismatchError{Files: make(map[string][]string)}
	for i, r := range reports {
		if r.AccountID == "" {
			continue
		}
		if _, seen := mismatch.Files[r.AccountID]; !seen {
			mismatch.Accounts = append(mismatch.Accounts, r.AccountID)
		}
		mismatch.Files[r.AccountID] = append(mismatch.Files[r.AccountID], sourceName(r, i))
	}
	if len(mismatch.Accounts) > 1 && !opts.AllowMultipleAccounts {
		return nil, mismatch
	}
	accounts := mismatch.Accounts
	multiple := len(accounts) > 1
	groups := make(map[string][]int) // account → indexes into sorted
	var groupOrder []string
	for i, r := range sorted {
		if _, ok := groups[r.AccountID]; !ok {
			groupOrder = append(groupOrder, r.AccountID)
		}
		groups[r.AccountID] = append(groups[r.AccountID], i)
	}

	merged := &ActivityReport{
		TradesBySymbol:                  make(map[string][]Trade),
		MarkToMarketPerformanceBySymbol: make(map[string][]MarkToMarketPerformance),
		OpeningPositions:                make(map[string]float64),
		BaseCurrency:                    baseCurrency,
		AccountID:                       strings.Join(accounts, ", "),
	}
	for i, r := range sorted {
		for _, warning := range r.Warnings {
			merged.Warnings = append(merged.Warnings, names[i]+": "+warning)
		}
		if r.AccountID == "" && len(accounts) > 0 {
			merged.Warnings = append(merged.Warnings, names[i]+" has no account number, so it could not be checked against the other statements' accounts")
		}
	}
	if multiple {
		merged.Warnings = append(merged.Warnings, fmt.Sprintf(
			"Combining %d accounts: position totals add the accounts together, but wash-sale replacements are matched within each account only", len(accounts)))
	}
	label := func(account string) string {
		if !multiple {
			return ""
		}
		if account == "" {
			return " for statements without an account number"
		}
		return " for account " + account
	}

	// owner picks the statement of the same account whose trades are used for a day.
	owner := func(day time.Time, account string) int {
		best := -1
		for _, i := range groups[account] {
			r := sorted[i]
			if day.Before(r.PeriodStart) || day.After(r.PeriodEnd) {
				continue
			}
			if best < 0 || !r.StatementDate.Before(sorted[best].StatementDate) {
				best = i
			}
		}
		return best
	}
	firstTrade := make(map[string]int) // inventory key → statement of its earliest kept trade
	firstTime := make(map[string]time.Time)
	for i, r := range sorted {
		source := StatementSource{Name: names[i], Period: r.Period, Start: r.PeriodStart, End: r.PeriodEnd, Generated: r.StatementDate, Trades: len(r.Trades)}
		for _, t := range r.Trades {
			if o := owner(washsale.Day(t.DateTime), r.AccountID); o >= 0 && o != i {
				continue
			}
			if t.Source == "" {
				t.Source = names[i]
			}
			merged.Trades = append(merged.Trades, t)
			source.TradesUsed++
			key := tradeInventoryKey(t)
			if when, seen := firstTime[key]; !seen || t.DateTime.Before(when) {
				firstTime[key], firstTrade[key] = t.DateTime, i
			}
		}
		merged.Sources = append(merged.Sources, source)
	}
	// A statement's reconciled opening quantity is the holding before its first
	// trade, so it holds for the combined trades when that trade comes first.
	for key, i := range firstTrade {
		if quantity, ok := sorted[i].OpeningPositions[key]; ok {
			merged.OpeningPositions[key] = quantity
		}
	}
	for _, t := range merged.Trades {
		merged.TradesBySymbol[t.Symbol] = append(merged.TradesBySymbol[t.Symbol], t)
	}
	for _, trades := range merged.TradesBySymbol {
		sort.SliceStable(trades, func(a, b int) bool { return trades[a].DateTime.Before(trades[b].DateTime) })
	}

	overlaps := false
	var coverages [][]DateRange
	for _, account := range groupOrder {
		members := groups[account]
		for x, i := range members {
			for _, j := range members[x+1:] {
				a, b := sorted[i], sorted[j]
				start, end := laterOf(a.PeriodStart, b.PeriodStart), earlierOf(a.PeriodEnd, b.PeriodEnd)
				if end.Before(start) {
					continue
				}
				overlaps = true
				span := DateRange{start, end}
				merged.Warnings = append(merged.Warnings, fmt.Sprintf(
					"%s and %s overlap from %s; trades on those days come from the most recently generated statement covering each day",
					names[i], names[j], span))
				if !sameTrades(tradesWithin(a.Trades, span), tradesWithin(b.Trades, span)) {
					merged.Warnings = append(merged.Warnings, fmt.Sprintf(
						"%s and %s disagree about trades from %s; check both statements", names[i], names[j], span))
				}
			}
		}
		var coverage []DateRange
		for _, i := range members {
			r := sorted[i]
			n := len(coverage)
			if n > 0 && !r.PeriodStart.After(coverage[n-1].End.AddDate(0, 0, 1)) {
				coverage[n-1].End = laterOf(coverage[n-1].End, r.PeriodEnd)
				continue
			}
			coverage = append(coverage, DateRange{r.PeriodStart, r.PeriodEnd})
		}
		for _, gap := range gapsBetween(coverage) {
			merged.Warnings = append(merged.Warnings, fmt.Sprintf(
				"No statement%s covers %s; trades from those days are missing and wash-sale windows touching them are not covered", label(account), gap))
		}
		coverages = append(coverages, coverage)
	}

	// Several accounts: a day counts as covered only when every account's statements cover it.
	merged.Coverage = coverages[0]
	for _, other := range coverages[1:] {
		merged.Coverage = intersectRanges(merged.Coverage, other)
	}
	first, last := sorted[0].PeriodStart, sorted[0].PeriodEnd
	for _, r := range sorted {
		last = laterOf(last, r.PeriodEnd)
		if r.StatementDate.After(merged.StatementDate) {
			merged.StatementDate = r.StatementDate
			merged.StatementLocation = r.StatementLocation
		} else if merged.StatementLocation == nil && r.StatementLocation != nil && r.StatementDate.Equal(merged.StatementDate) {
			merged.StatementLocation = r.StatementLocation
		}
	}
	merged.Period = first.Format("January 2, 2006") + " - " + last.Format("January 2, 2006")
	if len(merged.Coverage) > 0 {
		merged.PeriodStart = merged.Coverage[0].Start
		merged.PeriodEnd = merged.Coverage[len(merged.Coverage)-1].End
	}
	if multiple {
		parts := make([]string, len(merged.Coverage))
		for i, covered := range merged.Coverage {
			parts[i] = covered.String()
		}
		if len(parts) == 0 {
			merged.Warnings = append(merged.Warnings, "The accounts' statements share no dates, so wash-sale coverage is unknown")
		} else {
			merged.Warnings = append(merged.Warnings, "Wash-sale coverage counts only dates every account's statements cover: "+strings.Join(parts, "; "))
		}
	}
	merged.Today = merged.StatementDate
	if merged.Today.IsZero() {
		merged.Today = time.Now()
	}

	mergeMarkToMarket(merged, sorted, groups, groupOrder, overlaps)
	mergeChangeInNAV(merged, sorted, multiple, overlaps)
	return merged, nil
}

// mergeMarkToMarket builds one summary row per asset category and symbol: the
// opening quantity from each account's earliest statement, the ending quantity
// from each account's latest, added across accounts, and P/L added across
// statements when none overlap.
func mergeMarkToMarket(merged *ActivityReport, sorted []*ActivityReport, groups map[string][]int, groupOrder []string, overlaps bool) {
	type key struct{ asset, symbol string }
	var keys []key
	sums := make(map[key]*MarkToMarketPerformance)
	allHaveSummary := true
	for _, r := range sorted {
		allHaveSummary = allHaveSummary && len(r.MarkToMarketPerformance) > 0
		for _, row := range r.MarkToMarketPerformance {
			k := key{row.AssetCategory, row.Symbol}
			sum, ok := sums[k]
			if !ok {
				sum = &MarkToMarketPerformance{AssetCategory: row.AssetCategory, Symbol: row.Symbol}
				sums[k] = sum
				keys = append(keys, k)
			}
			sum.PositionPL += row.PositionPL
			sum.TransactionPL += row.TransactionPL
			sum.CommissionPL += row.CommissionPL
			sum.OtherPL += row.OtherPL
			sum.TotalPL += row.TotalPL
		}
	}
	if len(keys) == 0 {
		return
	}
	merged.MarkToMarketPLUnavailable = overlaps || !allHaveSummary
	if merged.MarkToMarketPLUnavailable {
		reason := "the statements overlap"
		if !overlaps {
			reason = "not every statement has one"
		}
		merged.Warnings = append(merged.Warnings, "Mark-to-Market P/L is not added up across statements because "+reason+"; per-security results use trade rows instead")
	}

	// Each account's earliest and latest statement.
	var firsts, lasts []*ActivityReport
	for _, account := range groupOrder {
		members := groups[account]
		first, last := sorted[members[0]], sorted[members[0]]
		for _, i := range members[1:] {
			r := sorted[i]
			if r.PeriodEnd.After(last.PeriodEnd) || (r.PeriodEnd.Equal(last.PeriodEnd) && !r.StatementDate.Before(last.StatementDate)) {
				last = r
			}
		}
		firsts, lasts = append(firsts, first), append(lasts, last)
	}
	// quantity totals a symbol's rows across statements. A statement whose summary
	// does not list a symbol held none of it; one without a summary is unknown.
	quantity := func(statements []*ActivityReport, k key, prior bool) (total, price float64, missing bool) {
		found := false
		for _, r := range statements {
			if len(r.MarkToMarketPerformance) == 0 {
				missing = true
				continue
			}
			for _, row := range r.MarkToMarketPerformance {
				if row.AssetCategory != k.asset || row.Symbol != k.symbol {
					continue
				}
				q, p, m := row.CurrentQuantity, row.CurrentPrice, row.CurrentQuantityMissing
				if prior {
					q, p, m = row.PriorQuantity, row.PriorPrice, row.PriorQuantityMissing
				}
				total += q
				missing = missing || m
				if !found {
					price = p
				}
				found = true
			}
		}
		return total, price, missing
	}
	for _, k := range keys {
		row := *sums[k]
		if merged.MarkToMarketPLUnavailable {
			row.PositionPL, row.TransactionPL, row.CommissionPL, row.OtherPL, row.TotalPL = 0, 0, 0, 0, 0
		}
		row.PriorQuantity, row.PriorPrice, row.PriorQuantityMissing = quantity(firsts, k, true)
		row.CurrentQuantity, row.CurrentPrice, row.CurrentQuantityMissing = quantity(lasts, k, false)
		merged.MarkToMarketPerformance = append(merged.MarkToMarketPerformance, row)
		if row.Symbol != "" {
			merged.MarkToMarketPerformanceBySymbol[row.Symbol] = append(merged.MarkToMarketPerformanceBySymbol[row.Symbol], row)
		}
	}
}

// mergeChangeInNAV adds up Change in NAV when one account's statements run back
// to back: starting value from the first, ending value from the last, and every
// flow added.
func mergeChangeInNAV(merged *ActivityReport, sorted []*ActivityReport, multiple, overlaps bool) {
	some, all := false, true
	for _, r := range sorted {
		some = some || r.ChangeInNAV.HasData()
		all = all && r.ChangeInNAV.HasData()
	}
	if !some {
		return
	}
	if multiple || overlaps || len(merged.Gaps()) > 0 || !all {
		merged.Warnings = append(merged.Warnings, "Change in NAV is not combined: it needs one account whose statements all have it, with no overlaps or gaps")
		return
	}
	fields := make(map[string]float64)
	for _, r := range sorted {
		for name, value := range r.ChangeInNAV.Fields {
			fields[name] += value
		}
	}
	fields["Starting Value"] = sorted[0].ChangeInNAV.Fields["Starting Value"]
	fields["Ending Value"] = sorted[len(sorted)-1].ChangeInNAV.Fields["Ending Value"]
	merged.ChangeInNAV = changeInNAVFromFields(fields)
}

func gapsBetween(ranges []DateRange) []DateRange {
	var gaps []DateRange
	for i := 1; i < len(ranges); i++ {
		gaps = append(gaps, DateRange{ranges[i-1].End.AddDate(0, 0, 1), ranges[i].Start.AddDate(0, 0, -1)})
	}
	return gaps
}

// intersectRanges keeps the days both sorted, non-overlapping range lists cover.
func intersectRanges(a, b []DateRange) []DateRange {
	var out []DateRange
	for i, j := 0, 0; i < len(a) && j < len(b); {
		start, end := laterOf(a[i].Start, b[j].Start), earlierOf(a[i].End, b[j].End)
		if !end.Before(start) {
			out = append(out, DateRange{start, end})
		}
		if a[i].End.Before(b[j].End) {
			i++
		} else {
			j++
		}
	}
	return out
}

func tradesWithin(trades []Trade, span DateRange) []Trade {
	var out []Trade
	for _, t := range trades {
		if day := washsale.Day(t.DateTime); !day.Before(span.Start) && !day.After(span.End) {
			out = append(out, t)
		}
	}
	return out
}

// sameTrades compares two statements' trades by inventory and day, tolerating
// different detail levels (orders against executions) and broker rounding.
func sameTrades(a, b []Trade) bool {
	group := func(trades []Trade) map[string][]Trade {
		out := make(map[string][]Trade)
		for _, t := range trades {
			out[tradeInventoryKey(t)] = append(out[tradeInventoryKey(t)], t)
		}
		return out
	}
	x, y := group(a), group(b)
	if len(x) != len(y) {
		return false
	}
	for key, trades := range x {
		if !equivalentTradeLevels(trades, y[key]) {
			return false
		}
	}
	return true
}

func sourceName(r *ActivityReport, index int) string {
	if len(r.Sources) > 0 && r.Sources[0].Name != "" {
		return r.Sources[0].Name
	}
	return fmt.Sprintf("statement %d", index+1)
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
