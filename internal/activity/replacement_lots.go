package activity

import (
	"fmt"
	"math"

	"github.com/mhpenta/ibkr-form8949/internal/washsale"
)

const quantityEpsilon = 1e-9

func ambiguousTradeOrder(a, b Trade) bool {
	return a.IsBuy != b.IsBuy && washsale.DaysBetween(a.DateTime, b.DateTime) == 0 &&
		(a.DateOnly || b.DateOnly || a.DateTime.Equal(b.DateTime))
}

type lotDisposal struct {
	tradeIndex int
	quantity   float64
}

type replacementLot struct {
	tradeIndex    int
	quantity      float64
	remaining     float64
	assignedUntil float64
	disposals     []lotDisposal
}

// inspectReplacementLots uses FIFO to reconstruct the supplied long inventory.
// This is evidence for review, not a recalculation of the broker's tax lots or
// disallowed amount. Missing inventory and other lot elections are explicit.
// trades must be in stable chronological order.
func inspectReplacementLots(trades []Trade) map[int]WashSaleAnalysis {
	groups := make(map[string][]int)
	for i, t := range trades {
		groups[tradeInventoryKey(t)] = append(groups[tradeInventoryKey(t)], i)
	}
	out := make(map[int]WashSaleAnalysis)
	for _, indices := range groups {
		local := make([]Trade, 0, len(indices))
		for _, i := range indices {
			local = append(local, trades[i])
		}
		for i, check := range inspectInventory(local) {
			out[indices[i]] = check
		}
	}
	return out
}

func inspectInventory(trades []Trade) map[int]WashSaleAnalysis {
	var lots []*replacementLot
	needsDetails := make(map[int]bool)
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
			lots = append(lots, &replacementLot{tradeIndex: i, quantity: trade.Quantity, remaining: trade.Quantity})
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
		for _, code := range []string{"LI", "SL", "HC", "ML", "MLG", "MLL", "MSG", "MSL"} {
			if trade.HasCode(code) {
				uncertainInventory = true
			}
		}
		toDispose := math.Abs(trade.Quantity)
		for _, lot := range lots {
			used := math.Min(toDispose, lot.remaining)
			if used <= quantityEpsilon {
				continue
			}
			lot.disposals = append(lot.disposals, lotDisposal{i, used})
			lot.remaining -= used
			toDispose -= used
		}
		if toDispose > quantityEpsilon {
			uncertainInventory = true
		}
		needsDetails[i] = uncertainInventory
		if trade.PositionKnownFlat {
			// Retire any uncertain residual inventory. A broker snapshot, reconciled
			// against all supplied quantity changes, establishes a new empty baseline.
			for _, lot := range lots {
				if lot.remaining > quantityEpsilon {
					lot.disposals = append(lot.disposals, lotDisposal{i, lot.remaining})
					lot.remaining = 0
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
		check := WashSaleAnalysis{Trade: sale}
		if sale.HasCode("O") {
			check.Reason = "Short-position or mixed opening/closing order requires broker lot details"
			checks[i] = check
			continue
		}
		needed := math.Abs(sale.Quantity)
		for _, lot := range lots {
			// FIFO dispositions consume a prefix of the purchased shares.
			// Replacement assignments also advance a cursor: shares already
			// matched to an earlier loss must not be reused for another loss.
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
			check.Reason = "Broker closed-lot details required; FIFO inventory is incomplete, same-day order is unknown, or a different lot method was used"
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
