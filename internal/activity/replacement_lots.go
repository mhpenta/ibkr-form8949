package activity

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

const quantityEpsilon = 1e-9

// closedLotTolerance allows broker rounding when closed lots are summed.
const closedLotTolerance = 1e-6

func ambiguousTradeOrder(a, b Trade) bool {
	return a.IsBuy != b.IsBuy && washsale.DaysBetween(a.DateTime, b.DateTime) == 0 &&
		(a.DateOnly || b.DateOnly || a.DateTime.Equal(b.DateTime))
}

// LotMethod names how a sale's shares leave the supplied inventory.
type LotMethod string

const (
	LotFIFO        LotMethod = "FIFO"
	LotLIFO        LotMethod = "LIFO"
	LotHighestCost LotMethod = "Highest cost"
	LotClosedLots  LotMethod = "Broker closed lots"
)

// IBKR marks each closing trade with its tax-basis election; no code is FIFO,
// IBKR's default. Elections that depend on holding period, gain/loss targets or
// a manual choice cannot be reproduced from trade rows alone.
var lotElections = []struct {
	code      string
	method    LotMethod
	supported bool
}{
	{"LI", LotLIFO, true},
	{"HC", LotHighestCost, true},
	{"SL", "Specific lot", false},
	{"ML", "Maximize losses", false},
	{"MLG", "Maximize long-term gain", false},
	{"MLL", "Maximize long-term loss", false},
	{"MSG", "Maximize short-term gain", false},
	{"MSL", "Maximize short-term loss", false},
}

// saleLotMethod returns how a sale takes shares and whether that order is
// known. Readable closed-lot rows whose quantities add up to the sale name the
// lots outright, whatever the election.
func saleLotMethod(t Trade) (LotMethod, bool) {
	method, known := LotFIFO, true
	for _, election := range lotElections {
		if t.HasCode(election.code) {
			method, known = election.method, election.supported
			break
		}
	}
	if len(t.ClosedLots) == 0 && !t.closedLotsInvalid {
		return method, known
	}
	total := 0.0
	for _, lot := range t.ClosedLots {
		total += lot.Quantity
	}
	if !t.closedLotsInvalid && math.Abs(total-math.Abs(t.Quantity)) <= closedLotTolerance {
		return LotClosedLots, true
	}
	return method, false
}

type lotDisposal struct {
	tradeIndex int
	quantity   float64
}

type replacementLot struct {
	tradeIndex    int // -1 for undated opening inventory
	quantity      float64
	remaining     float64
	costPerShare  float64 // Unknown for opening inventory
	assignedUntil float64
	disposals     []lotDisposal
}

func (l *replacementLot) opening() bool { return l.tradeIndex < 0 }

func (l *replacementLot) dispose(tradeIndex int, quantity float64) {
	l.disposals = append(l.disposals, lotDisposal{tradeIndex, quantity})
	l.remaining -= quantity
}

// disposalOrder lists lots in the order a sale takes them. Opening inventory
// is oldest, so FIFO takes it first and LIFO last; its cost is unknown, so
// highest cost puts it last and the caller treats the result as uncertain.
func disposalOrder(lots []*replacementLot, method LotMethod) []*replacementLot {
	order := slices.Clone(lots)
	switch method {
	case LotLIFO:
		slices.Reverse(order)
	case LotHighestCost:
		sort.SliceStable(order, func(i, j int) bool {
			if order[i].opening() != order[j].opening() {
				return !order[i].opening()
			}
			return order[i].costPerShare > order[j].costPerShare
		})
	}
	return order
}

// disposeClosedLots removes the broker's closed lots from inventory. A lot
// acquired on a supplied purchase day comes from that day's purchases; any
// other lot predates the supplied trades and comes from opening inventory, or
// from untracked shares when no opening quantity is known. It reports false
// when a supplied purchase day lacks the shares the broker says were sold.
func disposeClosedLots(lots []*replacementLot, trades []Trade, saleIndex int, closed []ClosedLot) bool {
	ok := true
	for _, closedLot := range closed {
		needed := closedLot.Quantity
		purchaseDay := false
		for _, lot := range lots {
			if lot.opening() || !washsale.Day(trades[lot.tradeIndex].DateTime).Equal(closedLot.Acquired) {
				continue
			}
			purchaseDay = true
			if used := math.Min(needed, lot.remaining); used > quantityEpsilon {
				lot.dispose(saleIndex, used)
				needed -= used
			}
		}
		if purchaseDay {
			ok = ok && needed <= closedLotTolerance
			continue
		}
		for _, lot := range lots {
			if used := math.Min(needed, lot.remaining); lot.opening() && used > quantityEpsilon {
				lot.dispose(saleIndex, used)
				needed -= used
			}
		}
	}
	return ok
}

// inspectReplacementLots reconstructs the supplied long inventory, taking each
// sale's shares by its lot method. opening holds reconciled opening quantities
// by inventory key; it may be nil. This is evidence for review, not a
// recalculation of the broker's tax lots or disallowed amount. Missing
// inventory and elections that cannot be reproduced are explicit.
// trades must be in stable chronological order.
func inspectReplacementLots(trades []Trade, opening map[string]float64) map[int]WashSaleAnalysis {
	groups := make(map[string][]int)
	for i, t := range trades {
		groups[tradeInventoryKey(t)] = append(groups[tradeInventoryKey(t)], i)
	}
	out := make(map[int]WashSaleAnalysis)
	for key, indices := range groups {
		local := make([]Trade, 0, len(indices))
		for _, i := range indices {
			local = append(local, trades[i])
		}
		for i, check := range inspectInventory(local, opening[key]) {
			out[indices[i]] = check
		}
	}
	return out
}

func inspectInventory(trades []Trade, opening float64) map[int]WashSaleAnalysis {
	var lots []*replacementLot
	if opening > quantityEpsilon {
		lots = append(lots, &replacementLot{tradeIndex: -1, quantity: opening, remaining: opening})
	}
	needsDetails := make(map[int]bool)
	methods := make(map[int]LotMethod)
	uncertainInventory := false
	for i, trade := range trades {
		for j, other := range trades {
			if i != j && ambiguousTradeOrder(trade, other) {
				uncertainInventory = true
			}
		}
		if trade.IsBuy && trade.Quantity > 0 {
			if trade.HasCode("C") {
				// Buying to cover is not a new long replacement. A combined
				// closing/opening order needs the broker's quantity split.
				if trade.HasCode("O") {
					uncertainInventory = true
				}
				continue
			}
			// Basis includes commissions. Later wash-sale basis adjustments are
			// not in the purchase row, so highest-cost order is an estimate.
			cost := trade.TradePrice
			if trade.Basis != 0 {
				cost = math.Abs(trade.Basis) / trade.Quantity
			}
			lots = append(lots, &replacementLot{tradeIndex: i, quantity: trade.Quantity, remaining: trade.Quantity, costPerShare: cost})
			continue
		}
		if !trade.IsSale {
			continue
		}
		if trade.HasCode("O") {
			// Opening a short is not a long disposition.
			needsDetails[i] = true
			if trade.HasCode("C") {
				uncertainInventory = true
			}
			continue
		}
		method, known := saleLotMethod(trade)
		methods[i] = method
		if !known {
			uncertainInventory = true
		}
		if method == LotClosedLots {
			if !disposeClosedLots(lots, trades, i, trade.ClosedLots) {
				uncertainInventory = true
			}
		} else {
			toDispose := math.Abs(trade.Quantity)
			for _, lot := range disposalOrder(lots, method) {
				if method == LotHighestCost && lot.opening() && lot.remaining > quantityEpsilon {
					// Opening shares have an unknown cost and might have ranked first.
					uncertainInventory = true
				}
				used := math.Min(toDispose, lot.remaining)
				if used <= quantityEpsilon {
					continue
				}
				lot.dispose(i, used)
				toDispose -= used
			}
			if toDispose > quantityEpsilon {
				uncertainInventory = true
			}
		}
		needsDetails[i] = uncertainInventory
		if trade.PositionKnownFlat {
			// Retire any uncertain residual inventory. A broker snapshot, reconciled
			// against all supplied quantity changes, establishes a new empty baseline.
			for _, lot := range lots {
				if lot.remaining > quantityEpsilon {
					lot.dispose(i, lot.remaining)
				}
			}
			uncertainInventory = false
		}
	}

	checks := make(map[int]WashSaleAnalysis)
	for i, sale := range trades {
		if !sale.IsLossSale() {
			continue
		}
		check := WashSaleAnalysis{Trade: sale, LotMethod: methods[i]}
		if sale.HasCode("O") {
			check.Reason = "Short-position or mixed opening/closing order requires broker lot details"
			checks[i] = check
			continue
		}
		needed := math.Abs(sale.Quantity)
		for _, lot := range lots {
			if lot.opening() {
				// Undated opening shares cannot be shown to fall inside the window.
				continue
			}
			// Dispositions consume a prefix of each lot's shares, whatever the
			// order in which lots are chosen. Replacement assignments also
			// advance a cursor: shares already matched to an earlier loss must
			// not be reused for another loss.
			consumed := 0.0
			for _, disposal := range lot.disposals {
				if disposal.tradeIndex <= i {
					consumed += disposal.quantity
				}
			}
			start := math.Max(consumed, lot.assignedUntil)
			available := lot.quantity - start
			if available <= quantityEpsilon {
				continue
			}
			buy := trades[lot.tradeIndex]
			days := washsale.DaysBetween(sale.DateTime, buy.DateTime)
			if check.NearestRepurchase == nil || math.Abs(float64(days)) < math.Abs(float64(check.DaysDifference)) {
				candidate := buy
				candidate.Quantity = available
				check.NearestRepurchase = &candidate
				check.DaysDifference = days
			}
			if days < -30 || days > 30 || needed <= quantityEpsilon {
				continue
			}
			matched := math.Min(needed, available)
			check.MatchedQuantity += matched
			lot.assignedUntil = start + matched
			needed -= matched
		}

		switch {
		case needsDetails[i]:
			check.Reason = "Broker closed-lot details required; supplied inventory is incomplete, same-day order is unknown, or the lot election cannot be reproduced"
		case check.MatchedQuantity > quantityEpsilon && needed > quantityEpsilon:
			check.Reason = fmt.Sprintf("Only %g candidate replacement shares for %g sold; verify the disallowed quantity", check.MatchedQuantity, math.Abs(sale.Quantity))
		case check.MatchedQuantity > quantityEpsilon:
			// Replacement evidence found, but adjusted order P/L does not
			// disclose the disallowed loss or which individual lots lost money.
		case check.NearestRepurchase == nil:
			check.Reason = "No unallocated replacement shares found in supplied trades"
		case math.Abs(float64(check.DaysDifference)) <= 45:
			check.Reason = "Just outside 30-day window"
		default:
			check.Reason = "No matching purchase within wash sale timeframe"
		}
		checks[i] = check
	}
	return checks
}
