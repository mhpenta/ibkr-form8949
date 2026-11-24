package activity

import (
	"math"
	"sort"
	"time"
)

// TradeAction is what a stock trade did to the position, read from IBKR's O/C codes.
// Trades without codes are treated as long buys and sales.
type TradeAction int

const (
	ActionBuy       TradeAction = iota // opens or adds to a long position
	ActionSell                         // closes or reduces a long position
	ActionShortSale                    // a sale coded O and not C: opens or adds to a short
	ActionCover                        // a purchase coded C: closes or reduces a short
)

// TradeActions lists every action in display order.
var TradeActions = []TradeAction{ActionBuy, ActionSell, ActionShortSale, ActionCover}

func (a TradeAction) String() string {
	switch a {
	case ActionSell:
		return "Sell"
	case ActionShortSale:
		return "Sell short"
	case ActionCover:
		return "Buy to cover"
	default:
		return "Buy"
	}
}

// ClassifyTrade reads a trade's action from its quantity sign and O/C codes.
func ClassifyTrade(t Trade) TradeAction {
	switch {
	case t.IsBuy && t.HasCode("C"):
		return ActionCover
	case t.IsSale && t.HasCode("O") && !t.HasCode("C"):
		return ActionShortSale
	case t.IsSale:
		return ActionSell
	default:
		return ActionBuy
	}
}

// SideStats summarizes a group of trades on one side of the market.
type SideStats struct {
	Trades     int
	Shares     float64
	Value      float64 // shares × trade price, before commissions
	NetValue   float64 // cash paid including commissions (buys) or received after them (sells)
	Commission float64 // commissions and fees paid, as a positive amount
	RealizedPL float64 // broker-reported realized P/L

	LowPrice, MedianPrice, HighPrice         float64 // MedianPrice is weighted by shares
	SmallestTrade, MedianTrade, LargestTrade float64
	First, Last                              time.Time
}

// AveragePrice is the volume-weighted average trade price.
func (s SideStats) AveragePrice() float64 { return ratio(s.Value, s.Shares) }

// AverageNetPrice is the average price per share including commissions.
func (s SideStats) AverageNetPrice() float64 { return ratio(s.NetValue, s.Shares) }

// SummarizeSide totals a group of trades that all buy or all sell.
func SummarizeSide(trades []Trade) SideStats {
	var s SideStats
	if len(trades) == 0 {
		return s
	}
	sizes := make([]float64, len(trades))
	byPrice := append([]Trade(nil), trades...)
	sort.SliceStable(byPrice, func(i, j int) bool { return byPrice[i].TradePrice < byPrice[j].TradePrice })
	for i, t := range trades {
		shares := math.Abs(t.Quantity)
		sizes[i] = shares
		s.Trades++
		s.Shares += shares
		s.Value += shares * t.TradePrice
		s.Commission -= t.CommFee
		s.RealizedPL += t.RealizedPL
		if t.IsBuy {
			s.NetValue += -t.Proceeds - t.CommFee
		} else {
			s.NetValue += t.Proceeds + t.CommFee
		}
		if s.First.IsZero() || t.DateTime.Before(s.First) {
			s.First = t.DateTime
		}
		if t.DateTime.After(s.Last) {
			s.Last = t.DateTime
		}
	}
	s.LowPrice, s.HighPrice = byPrice[0].TradePrice, byPrice[len(byPrice)-1].TradePrice
	half, seen := s.Shares/2, 0.0
	for _, t := range byPrice {
		seen += math.Abs(t.Quantity)
		if seen >= half-quantityEpsilon {
			s.MedianPrice = t.TradePrice
			break
		}
	}
	sort.Float64s(sizes)
	s.SmallestTrade, s.LargestTrade = sizes[0], sizes[len(sizes)-1]
	if n := len(sizes); n%2 == 1 {
		s.MedianTrade = sizes[n/2]
	} else {
		s.MedianTrade = (sizes[n/2-1] + sizes[n/2]) / 2
	}
	return s
}

// PositionStep is one trade in a position timeline with the position after it.
type PositionStep struct {
	Trade    Trade
	Action   TradeAction
	Position float64 // shares held after the trade; negative when short
	// OpenPrice is the average trade price of the shares still open, by the
	// average-cost method, excluding commissions. It is unknown (OpenPriceKnown
	// false) while shares held before the first supplied trade remain open.
	OpenPrice      float64
	OpenPriceKnown bool
}

// MonthStats is one calendar month of trades.
type MonthStats struct {
	Month       time.Time // first day of the month
	Buys, Sells SideStats
	EndPosition float64 // position after the month's last trade
}

// PriceBand counts the shares traded within one price range [Low, High).
type PriceBand struct {
	Low, High             float64
	Bought, Sold          float64
	BuyTrades, SellTrades int
}

// PositionStats summarizes one security's trades.
type PositionStats struct {
	Buys, Sells SideStats                 // every purchase and every sale
	Actions     map[TradeAction]SideStats // the same trades split by action
	// SoldCost is the broker's reported basis for long sales (ActionSell),
	// which includes shares bought before the supplied trades.
	SoldShares, SoldCost float64
	Steps                []PositionStep
	Months               []MonthStats
	PriceBands           []PriceBand
}

// HasShortActivity reports short sales or covers among the trades.
func (p PositionStats) HasShortActivity() bool {
	return p.Actions[ActionShortSale].Trades+p.Actions[ActionCover].Trades > 0
}

// AverageSoldCost is the average broker basis per share of long shares sold.
func (p PositionStats) AverageSoldCost() float64 { return ratio(p.SoldCost, p.SoldShares) }

// Final is the last timeline step, or a zero step when there are no trades.
func (p PositionStats) Final(opening float64) PositionStep {
	if len(p.Steps) == 0 {
		return PositionStep{Position: opening, OpenPriceKnown: math.Abs(opening) <= quantityEpsilon}
	}
	return p.Steps[len(p.Steps)-1]
}

// AnalyzePosition summarizes one security's trades. opening is the quantity held
// before the first trade; pass 0 when it is unknown and say so to the reader.
func AnalyzePosition(trades []Trade, opening float64) PositionStats {
	ordered := append([]Trade(nil), trades...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].DateTime.Before(ordered[j].DateTime) })

	stats := PositionStats{Actions: make(map[TradeAction]SideStats)}
	var buys, sells []Trade
	byAction := make(map[TradeAction][]Trade)
	for _, t := range ordered {
		action := ClassifyTrade(t)
		byAction[action] = append(byAction[action], t)
		if t.IsBuy {
			buys = append(buys, t)
		} else if t.IsSale {
			sells = append(sells, t)
		}
		if action == ActionSell {
			stats.SoldShares += math.Abs(t.Quantity)
			stats.SoldCost += math.Abs(t.Basis)
		}
	}
	stats.Buys, stats.Sells = SummarizeSide(buys), SummarizeSide(sells)
	for _, action := range TradeActions {
		stats.Actions[action] = SummarizeSide(byAction[action])
	}
	stats.Steps = positionSteps(ordered, opening)
	stats.Months = monthlyStats(stats.Steps)
	stats.PriceBands = priceBands(ordered)
	return stats
}

func positionSteps(trades []Trade, opening float64) []PositionStep {
	steps := make([]PositionStep, 0, len(trades))
	position, average := opening, 0.0
	known := math.Abs(opening) <= quantityEpsilon
	for _, t := range trades {
		q := t.Quantity
		switch {
		case q == 0:
		case math.Abs(position) <= quantityEpsilon || (position > 0) == (q > 0):
			// Opening or adding: blend the new shares into the average.
			if math.Abs(position) <= quantityEpsilon {
				position, average, known = 0, 0, true
			}
			if known {
				average = (average*math.Abs(position) + t.TradePrice*math.Abs(q)) / (math.Abs(position) + math.Abs(q))
			}
			position += q
		case math.Abs(q) <= math.Abs(position)+quantityEpsilon:
			// Reducing: the average of the remaining shares is unchanged.
			position += q
			if math.Abs(position) <= quantityEpsilon {
				position, average, known = 0, 0, true
			}
		default:
			// Crossing zero: the remainder opens a new position at this price.
			position += q
			average, known = t.TradePrice, true
		}
		step := PositionStep{Trade: t, Action: ClassifyTrade(t), Position: position, OpenPriceKnown: known}
		if known && math.Abs(position) > quantityEpsilon {
			step.OpenPrice = average
		}
		steps = append(steps, step)
	}
	return steps
}

func monthlyStats(steps []PositionStep) []MonthStats {
	var months []MonthStats
	var buys, sells []Trade
	flush := func(month time.Time, end float64) {
		months = append(months, MonthStats{Month: month, Buys: SummarizeSide(buys), Sells: SummarizeSide(sells), EndPosition: end})
		buys, sells = nil, nil
	}
	for i, step := range steps {
		t := step.Trade
		if t.IsBuy {
			buys = append(buys, t)
		} else if t.IsSale {
			sells = append(sells, t)
		}
		month := firstOfMonth(t.DateTime)
		if i == len(steps)-1 || !firstOfMonth(steps[i+1].Trade.DateTime).Equal(month) {
			flush(month, step.Position)
		}
	}
	return months
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// priceBands groups traded shares into about ten equal price ranges with round edges.
func priceBands(trades []Trade) []PriceBand {
	var low, high float64
	first := true
	for _, t := range trades {
		if t.Quantity == 0 {
			continue
		}
		if first || t.TradePrice < low {
			low = t.TradePrice
		}
		if first || t.TradePrice > high {
			high = t.TradePrice
		}
		first = false
	}
	if first {
		return nil
	}
	const fuzz = 1e-9
	var bands []PriceBand
	var start, step float64
	if high-low <= fuzz {
		bands = []PriceBand{{Low: low, High: high}}
	} else {
		step = niceStep((high - low) / 10)
		start = math.Floor(low/step+fuzz) * step
		n := int(math.Floor((high-start)/step+fuzz)) + 1
		for i := 0; i < n; i++ {
			bands = append(bands, PriceBand{Low: roundEdge(start + float64(i)*step), High: roundEdge(start + float64(i+1)*step)})
		}
	}
	for _, t := range trades {
		i := 0
		if step > 0 {
			i = min(max(int(math.Floor((t.TradePrice-start)/step+fuzz)), 0), len(bands)-1)
		}
		shares := math.Abs(t.Quantity)
		if t.IsBuy {
			bands[i].Bought += shares
			bands[i].BuyTrades++
		} else if t.IsSale {
			bands[i].Sold += shares
			bands[i].SellTrades++
		}
	}
	return bands
}

// niceStep rounds raw up to 1, 2, 2.5 or 5 times a power of ten.
func niceStep(raw float64) float64 {
	magnitude := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5} {
		if m*magnitude >= raw*(1-1e-12) {
			return m * magnitude
		}
	}
	return 10 * magnitude
}

func roundEdge(x float64) float64 { return math.Round(x*1e8) / 1e8 }

func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
