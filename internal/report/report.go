package report

import (
	"fmt"
	"io"
	"strings"

	"dbtrace/internal/config"
	"dbtrace/internal/diff"
)

func Print(w io.Writer, result diff.Result, cfg config.ReportConfig) {
	if len(result.Tables) == 0 {
		fmt.Fprintln(w, "No database changes detected.")
		return
	}

	fmt.Fprintln(w, "RESULT:")
	fmt.Fprintf(w, "%d tables changed\n\n", len(result.Tables))

	for i, table := range result.Tables {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "► Table: %s\n", table.Table)
		printOperation(w, "Inserts", table.Inserted, cfg)
		printOperation(w, "Updates", table.Updated, cfg)
		printOperation(w, "Deletes", table.Deleted, cfg)
	}
}

func printOperation(w io.Writer, name string, rows []diff.RowChange, cfg config.ReportConfig) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "  * %s (%d)\n", name, len(rows))

	limit := cfg.MaxLinesPerOperation
	if limit <= 0 {
		limit = 50
	}
	printed := 0
	total := 0
	for _, row := range rows {
		identity := rowIdentity(row)
		for _, change := range row.Changes {
			total++
			if printed >= limit {
				continue
			}
			fmt.Fprintf(w, "       %s %s: %s → %s\n", identity, change.Column, formatValue(change.Before, change.BeforeNull, cfg), formatValue(change.After, change.AfterNull, cfg))
			printed++
		}
	}
	if total > printed {
		fmt.Fprintf(w, "       ... and %d more\n", total-printed)
	}
}

func rowIdentity(row diff.RowChange) string {
	if strings.Contains(row.PK, "=") {
		return row.PK
	}
	if row.PKColumn != "" && !strings.Contains(row.PKColumn, ",") {
		return row.PKColumn + "=" + row.PK
	}
	return "pk=" + row.PK
}

func formatValue(value string, isNull bool, cfg config.ReportConfig) string {
	if isNull {
		return "NULL"
	}
	limit := cfg.MaxValueLength
	if limit <= 0 {
		limit = 200
	}
	value = strings.ReplaceAll(value, "\r\n", "\\n")
	value = strings.ReplaceAll(value, "\n", "\\n")
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
