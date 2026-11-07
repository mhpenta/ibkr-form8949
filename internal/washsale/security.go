package washsale

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	sharePrefix = regexp.MustCompile(`(?i)^([0-9][0-9,]*(?:\.[0-9]+)?)\s+(?:sh|shares)\.?\s+(.+)$`)
	shareSuffix = regexp.MustCompile(`(?i)^(.+?)\s+([0-9][0-9,]*(?:\.[0-9]+)?)\s+(?:sh|shares)\.?$`)
)

// Security separates explicit share quantities from the security description.
// Preserve option expiration, strike and type; collapsing to an underlying
// ticker would incorrectly equate different contracts. Zero means unknown qty.
func Security(description string) (identity string, quantity float64) {
	identity = strings.Join(strings.Fields(description), " ")
	if match := sharePrefix.FindStringSubmatch(identity); match != nil {
		identity = match[2]
		quantity, _ = strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
	} else if match := shareSuffix.FindStringSubmatch(identity); match != nil {
		identity = match[1]
		quantity, _ = strconv.ParseFloat(strings.ReplaceAll(match[2], ",", ""), 64)
	}
	return strings.ToUpper(identity), quantity
}
