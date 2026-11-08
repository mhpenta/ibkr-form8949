package form8949

import (
	"os"
	"testing"
	"time"
)

func TestParseCSVLine(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Basic CSV line",
			input:    "Data,100 sh TEST SECURITY,01/01/2024,02/01/2024,900.00,1000.00,,100.00,0.00",
			expected: []string{"Data", "100 sh TEST SECURITY", "01/01/2024", "02/01/2024", "900.00", "1000.00", "", "100.00", "0.00"},
		},
		{
			name:     "CSV line with quotes",
			input:    "Data,\"100 sh TEST, SECURITY\",01/01/2024,02/01/2024,\"900,00\",\"1,000.00\",,\"100.00\",\"0.00\"",
			expected: []string{"Data", "100 sh TEST, SECURITY", "01/01/2024", "02/01/2024", "900,00", "1,000.00", "", "100.00", "0.00"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseCSVLine(tc.input)

			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d fields, got %d", len(tc.expected), len(result))
				return
			}

			for i := range result {
				if result[i] != tc.expected[i] {
					t.Errorf("Field %d: expected '%s', got '%s'", i, tc.expected[i], result[i])
				}
			}
		})
	}
}

func TestParseAmount(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected float64
	}{
		{
			name:     "Simple number",
			input:    "123.45",
			expected: 123.45,
		},
		{
			name:     "Number with commas",
			input:    "1,234.56",
			expected: 1234.56,
		},
		{
			name:     "Number with dollar sign",
			input:    "$1,234.56",
			expected: 1234.56,
		},
		{
			name:     "Negative in parentheses",
			input:    "(123.45)",
			expected: -123.45,
		},
		{
			name:     "Full format negative",
			input:    "($1,234.56)",
			expected: -1234.56,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseAmount(tc.input)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tc.expected {
				t.Errorf("Expected %.2f, got %.2f", tc.expected, result)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected time.Time
	}{
		{
			name:     "MM/DD/YYYY format",
			input:    "12/31/2024",
			expected: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "MM-DD-YYYY format",
			input:    "12-31-2024",
			expected: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "YYYY-MM-DD format",
			input:    "2024-12-31",
			expected: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "VARIOUS date",
			input:    "VARIOUS",
			expected: time.Time{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.input == "VARIOUS" {
				// Special case for VARIOUS as it uses current year
				result, err := parseDate(tc.input)
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}

				// Just check month and day
				if !result.IsZero() {
					t.Errorf("VARIOUS date should remain unknown, got %s", result)
				}
			} else {
				result, err := parseDate(tc.input)
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}

				if !result.Equal(tc.expected) {
					t.Errorf("Expected %s, got %s", tc.expected.Format("2006-01-02"), result.Format("2006-01-02"))
				}
			}
		})
	}
}

// TestParseCSVFile tests the end-to-end CSV parsing
func TestParseCSVFile(t *testing.T) {
	// Create a temporary test file
	testContent := `Title,Worksheet for Form 8949,2024,
Account,U1234567,Test Account,
Part,I,
Box,A,
Header,Description,Date Acquired,Date Sold,Proceeds,Cost Basis,Code(s),Adjustment,Gain or (loss),
Data,100 sh TEST SECURITY,01/01/2024,02/01/2024,"900.00","1,000.00",W,"100.00","0.00",
Data,100 sh TEST SECURITY VARIOUS,VARIOUS,02/01/2024,"900.00","1,000.00",W,"100.00","0.00",
Footer,,,,"1,800.00","2,000.00",,"200.00","0.00",`

	tmpFile, err := os.CreateTemp("", "test-*.csv")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(testContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Parse the file
	transactions, year, err := ParseCSVFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseCSVFile failed: %v", err)
	}

	// Verify year extraction
	if year != 2024 {
		t.Errorf("Expected year 2024, got %d", year)
	}

	// Verify number of transactions
	if len(transactions) != 2 {
		t.Errorf("Expected 2 transactions, got %d", len(transactions))
	}

	// Verify transaction details
	if len(transactions) >= 1 {
		tx := transactions[0]
		if tx.Description != "100 sh TEST SECURITY" {
			t.Errorf("Expected description '100 sh TEST SECURITY', got '%s'", tx.Description)
		}
		if tx.Proceeds != 900.0 {
			t.Errorf("Expected proceeds 900.0, got %.2f", tx.Proceeds)
		}
		if tx.CostBasis != 1000.0 {
			t.Errorf("Expected cost basis 1000.0, got %.2f", tx.CostBasis)
		}
		if tx.Adjustment != 100.0 {
			t.Errorf("Expected adjustment 100.0, got %.2f", tx.Adjustment)
		}
		if !tx.IsWashSale {
			t.Errorf("Transaction should be a wash sale")
		}
	}

	// Verify VARIOUS lot handling
	if len(transactions) >= 2 {
		tx := transactions[1]
		if !tx.HasVariousLot {
			t.Errorf("Transaction should have HasVariousLot=true")
		}
		if tx.IsLongTerm {
			t.Errorf("VARIOUS lots should default to short term")
		}
	}
}
