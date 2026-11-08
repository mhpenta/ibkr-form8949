# ibkr-form8949

Command-line tools for checking Interactive Brokers tax and activity exports for
questionable wash-sale adjustments, and for reviewing trading performance.

> [!IMPORTANT]
> **Not tax, legal, or financial advice.** This is an independent inspection tool, not
> affiliated with Interactive Brokers. It does not prepare returns or recalculate cost basis,
> it only sees the files you give it, and its output may be incomplete or wrong. Check anything
> it flags against your broker's official documents and a qualified tax professional. Use at
> your own risk; see [LICENSE](LICENSE).

> [!NOTE]
> **Your data stays on your machine.** The tools read local CSV files and make no network
> requests. Statements contain account numbers and your full trading history: never attach
> them to issues. Reproduce problems with made-up trades instead.

## Install

```bash
go install github.com/mhpenta/ibkr-form8949@latest
go install github.com/mhpenta/ibkr-form8949/cmd/ibkr-activity@latest
```

Or clone the repository and run `make build`. Requires Go 1.21 or newer.

## ibkr-form8949: Form 8949 worksheets

Reads the Form 8949 CSV export and flags wash-sale adjustments that have no supporting
repurchase in the file: none within 30 days, just outside the window (31–35 days), or further
out but still nearby.

```bash
ibkr-form8949 --file 2024.csv                          # summary and suspicious wash sales
ibkr-form8949 --file 2024.csv --focus no-repurchase    # only wash sales with no repurchase
ibkr-form8949 --file 2024.csv --report quarterly       # also: position, lots, position-performance, all
```

## ibkr-activity: activity statements

Reads an activity statement CSV for per-security performance, position checks, and the
loss-sale windows still open.

```bash
ibkr-activity --file activity.2024.csv --analyze-all                     # performance by security
ibkr-activity --file activity.2024.csv --analyze-all --xlsx 2024.xlsx    # ...and every security in a spreadsheet
ibkr-activity --file activity.2024.csv --symbol AAPL --analyze-position  # one security in detail
ibkr-activity --file activity.2024.csv --wash-sale-periods               # open loss-sale windows
ibkr-activity --file activity.2024.csv --safe-repurchase                 # first dates outside them
```

`--xlsx` writes a workbook with a sortable, filterable sheet of every security (the terminal
shows only the top and bottom 20) plus a summary sheet. To open it in Google Sheets, drag the
file into Google Drive, or use File → Import → Upload in a sheet. It also opens in Excel and
Numbers.

All flags and the detailed matching rules are in [REFERENCE.md](REFERENCE.md).

## What it can't see

- **A Form 8949 export lists sold lots, not every purchase.** Shares you still hold, other
  accounts, and trades outside the file can't be checked. A missing match is something to
  review, not proof of a broker error.
- **Activity-statement replacement shares are estimated with FIFO.** Partial lots, short
  positions, and other lot elections need the broker's closed-lot details.
- **Repurchase dates describe the supplied sales only.** They don't establish that buying
  back today is safe.

## Layout

```
main.go                 ibkr-form8949 command
cmd/ibkr-activity/      ibkr-activity command
internal/form8949/      Form 8949 parsing and wash-sale analysis
internal/report/        Form 8949 reports
internal/activity/      activity statement parsing and analysis
internal/washsale/      window and security-matching rules shared by both
internal/xlsx/          dependency-free .xlsx writer for spreadsheet output
```

`make test` runs the test suite. The only statement-shaped fixture,
`internal/activity/testdata/activity_layouts_anonymized.csv`, keeps a real statement's column
layout with every identifier and value replaced.

## License

[MIT](LICENSE)
