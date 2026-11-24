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
ibkr-activity --file activity.2024.csv --symbol AAPL --analyze-position --xlsx aapl.xlsx  # ...as a workbook
ibkr-activity --file activity.2024.csv --wash-sale-periods               # open loss-sale windows
ibkr-activity --file activity.2024.csv --safe-repurchase                 # first dates outside them
```

`--analyze-position` shows average buy and sell prices (weighted by shares, with and without
commissions), price and trade-size ranges, IBKR's average cost of the shares sold, and the
average price of the shares still open. Add `--print-all-transactions` to list every trade with
the running position.

`--xlsx` writes a workbook. With `--analyze-all` it holds a sortable, filterable sheet of every
security (the terminal shows only the top and bottom 20) plus a summary. With
`--analyze-position` it holds a summary, every transaction with the running position, monthly
activity, shares traded by price range, and each loss sale with its 30-day window. To open it
in Google Sheets, drag the file into Google Drive, or use File → Import → Upload in a sheet. It
also opens in Excel and Numbers.

### Combining statements

Pass several statements to look at them as one history: repeat `--file`, or list the files
after it (a shell glob works).

```bash
ibkr-activity --file 2023.csv --file 2024.csv --symbol AAPL --analyze-position
ibkr-activity --file statements/*.csv --wash-sale-periods
```

> [!WARNING]
> **Combining is best effort, with simple rules.** Where statement periods overlap, each day's
> trades are taken from the most recently generated statement covering that day. Days no
> statement covers are treated as unknown, and wash-sale windows touching them are reported as
> not covered. Overlaps, gaps and statements that disagree about the same days all produce
> warnings. Read them, and prefer statements that run back to back. Splits, transfers and other
> corporate actions between statements are not reconciled.
>
> Statements from different accounts are refused, with a message listing which file belongs to
> which account. Add `--allow-multiple-accounts` to combine them on purpose.

All flags and the detailed matching rules are in [REFERENCE.md](REFERENCE.md).

## What it can't see

- **A Form 8949 export lists sold lots, not every purchase.** Shares you still hold, other
  accounts, and trades outside the file can't be checked. A missing match is something to
  review, not proof of a broker error.
- **Activity-statement replacement shares follow each sale's lot election.** FIFO, LIFO and
  highest cost are read from IBKR's trade codes; exported closed-lot rows are used exactly.
  Specific-lot and maximize/minimize elections, partial lots and short positions need the
  broker's closed-lot details.
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
