package report

import (
	"fmt"
	"sort"

	"github.com/mhpenta/ibkr-form8949/internal/form8949"
)

// PrintLotsReport prints a detailed lots-based analysis
func PrintLotsReport(analysis form8949.TaxAnalysis) {
	fmt.Println("\n=== DETAILED LOTS ANALYSIS ===")
	fmt.Println("This report provides a detailed analysis of individual lots traded in the tax year")

	// Create a map to store security statistics
	type SecurityStat struct {
		Name          string
		Count         int
		TotalProceeds float64
		TotalCost     float64
		TotalPL       float64
	}

	// Create a map for security statistics based on PositionAnalysis
	securitiesBySize := make([]SecurityStat, 0, len(analysis.PositionAnalysis))

	// Process all positions to create security statistics
	for _, pos := range analysis.PositionAnalysis {
		securitiesBySize = append(securitiesBySize, SecurityStat{
			Name:          pos.SecurityName,
			Count:         pos.TransactionCount,
			TotalProceeds: pos.TotalProceeds,
			TotalCost:     pos.TotalCostBasis,
			TotalPL:       pos.TotalGainOrLoss,
		})
	}

	sort.Slice(securitiesBySize, func(i, j int) bool {
		if securitiesBySize[i].Count == securitiesBySize[j].Count {
			return securitiesBySize[i].Name < securitiesBySize[j].Name
		}
		return securitiesBySize[i].Count > securitiesBySize[j].Count
	})

	// Print by lot count
	fmt.Println("\nSECURITIES BY NUMBER OF LOTS (descending)")
	fmt.Println("Security | Lots | Total Proceeds | Total Cost | Total P/L")
	fmt.Println("-----------------------------------------------------------")

	// Print top 15 securities by lot count
	for i, sec := range securitiesBySize {
		if i < 15 {
			fmt.Printf("%s | %d | $%.2f | $%.2f | $%.2f\n",
				sec.Name, sec.Count, sec.TotalProceeds, sec.TotalCost, sec.TotalPL)
		}
	}

	// Print lot details for positions with more than 3 lots
	for _, sec := range securitiesBySize {
		if sec.Count > 3 {
			fmt.Printf("\n=== %s (%d lots) ===\n", sec.Name, sec.Count)

			// Calculate average metrics
			avgPL := sec.TotalPL / float64(sec.Count)
			avgCost := sec.TotalCost / float64(sec.Count)
			avgProceeds := sec.TotalProceeds / float64(sec.Count)

			// Print summary information
			fmt.Printf("Total lots: %d\n", sec.Count)
			fmt.Printf("Average P/L per lot: $%.2f\n", avgPL)
			fmt.Printf("Average cost basis: $%.2f\n", avgCost)
			fmt.Printf("Average proceeds: $%.2f\n", avgProceeds)
			fmt.Printf("Total P/L: $%.2f\n", sec.TotalPL)
		}
	}

	fmt.Println("\nHOLDING PERIOD ANALYSIS")
	type bucket struct {
		count int
		pl    float64
	}
	var terms [3]bucket
	var various bucket
	var months [12]bucket
	for _, tx := range analysis.Transactions {
		index := 0
		if tx.TermUnknown {
			index = 2
		} else if tx.IsLongTerm {
			index = 1
		}
		terms[index].count++
		terms[index].pl += tx.GainOrLoss
		if tx.HasVariousLot {
			various.count++
			various.pl += tx.GainOrLoss
		}
		month := int(tx.DateSold.Month()) - 1
		months[month].count++
		months[month].pl += tx.GainOrLoss
	}
	fmt.Println("Holding Period | Transaction Count | Total P/L")
	for i, name := range []string{"Short-term", "Long-term", "Unknown term"} {
		fmt.Printf("%s | %d | $%.2f\n", name, terms[i].count, terms[i].pl)
	}
	fmt.Printf("VARIOUS acquisitions (included above) | %d | $%.2f\n", various.count, various.pl)
	fmt.Println("\nMONTHLY TRADING ACTIVITY")
	fmt.Println("Month | Transactions | Realized P/L")
	for i, month := range months {
		if month.count > 0 {
			fmt.Printf("%02d | %d | $%.2f\n", i+1, month.count, month.pl)
		}
	}
	PrintDisclaimer()
}
