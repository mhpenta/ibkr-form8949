package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormCLIYearAndInputValidation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "form")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	year := time.Now().Year()
	path := filepath.Join(t.TempDir(), "worksheet.csv")
	input := fmt.Sprintf("Title,Worksheet for Form 8949,%d,\nHeader,Description,Date Acquired,Date Sold,Proceeds,Cost Basis,Code(s),Adjustment,Gain or (loss)\nData,XYZ,01/01/%d,02/01/%d,1100,1000,,0,100\nData,XYZ,01/01/%d,02/01/%d,1600,1000,,0,600\n", year-1, year-1, year-1, year, year)
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		flags   []string
		success bool
		want    string
	}{
		{"explicit current year", []string{"--year", fmt.Sprint(year)}, true, "600.00"},
		{"header default", nil, true, "100.00"},
		{"empty selected year", []string{"--year", fmt.Sprint(year - 2)}, false, "No dispositions found for selected year"},
		{"reversed windows", []string{"--extended-window", "50", "--max-window", "40"}, false, "Invalid flags"},
		{"too short window", []string{"--extended-window", "29"}, false, "Invalid flags"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--file", path}, tc.flags...)
			out, err := exec.Command(binary, args...).CombinedOutput()
			if (err == nil) != tc.success || !strings.Contains(string(out), tc.want) {
				t.Fatalf("err=%v\n%s", err, out)
			}
		})
	}
}
