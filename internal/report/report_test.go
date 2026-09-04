package report

import (
	"bytes"
	"strings"
	"testing"

	"dbtrace/internal/config"
	"dbtrace/internal/diff"
)

func TestPrintGroupedValueDiff(t *testing.T) {
	result := diff.Result{
		Tables: []diff.TableDiff{
			{
				Table: "users",
				Updated: []diff.RowChange{
					{
						PKColumn: "id",
						PK:       "1",
						Changes: []diff.ColumnChange{
							{
								Column: "last_login",
								Before: "2025-06-02 07:26:58",
								After:  "2025-06-15 09:22:35",
							},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	Print(&buf, result, config.ReportConfig{MaxLinesPerOperation: 50, MaxValueLength: 200})

	got := buf.String()
	for _, want := range []string{
		"RESULT:",
		"1 tables changed",
		"► Table: users",
		"  * Updates (1)",
		"       id=1 last_login: 2025-06-02 07:26:58 → 2025-06-15 09:22:35",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q\n%s", want, got)
		}
	}
}

func TestPrintNoChanges(t *testing.T) {
	var buf bytes.Buffer
	Print(&buf, diff.Result{}, config.ReportConfig{})

	got := buf.String()
	want := "No database changes detected.\n"
	if got != want {
		t.Fatalf("report = %q, want %q", got, want)
	}
}

func TestPrintInsertDeleteNullValues(t *testing.T) {
	result := diff.Result{
		Tables: []diff.TableDiff{
			{
				Table: "payments",
				Inserted: []diff.RowChange{
					{
						PKColumn: "id",
						PK:       "991",
						Changes: []diff.ColumnChange{
							{Column: "id", BeforeNull: true, After: "991"},
							{Column: "amount", BeforeNull: true, After: "49.00"},
						},
					},
				},
				Deleted: []diff.RowChange{
					{
						PKColumn: "id",
						PK:       "990",
						Changes: []diff.ColumnChange{
							{Column: "id", Before: "990", AfterNull: true},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	Print(&buf, result, config.ReportConfig{MaxLinesPerOperation: 50, MaxValueLength: 200})

	got := buf.String()
	for _, want := range []string{
		"► Table: payments",
		"  * Inserts (1)",
		"       id=991 id: NULL → 991",
		"       id=991 amount: NULL → 49.00",
		"  * Deletes (1)",
		"       id=990 id: 990 → NULL",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q\n%s", want, got)
		}
	}
}

func TestPrintLimitsChangedColumnLines(t *testing.T) {
	result := diff.Result{
		Tables: []diff.TableDiff{
			{
				Table: "users",
				Updated: []diff.RowChange{
					{
						PKColumn: "id",
						PK:       "1",
						Changes: []diff.ColumnChange{
							{Column: "first", Before: "a", After: "b"},
							{Column: "second", Before: "c", After: "d"},
							{Column: "third", Before: "e", After: "f"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	Print(&buf, result, config.ReportConfig{MaxLinesPerOperation: 2, MaxValueLength: 200})

	got := buf.String()
	for _, want := range []string{
		"       id=1 first: a → b",
		"       id=1 second: c → d",
		"       ... and 1 more",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "id=1 third: e → f") {
		t.Fatalf("report should have limited third line\n%s", got)
	}
}

func TestPrintTruncatesAndEscapesValues(t *testing.T) {
	result := diff.Result{
		Tables: []diff.TableDiff{
			{
				Table: "users",
				Updated: []diff.RowChange{
					{
						PKColumn: "id",
						PK:       "1",
						Changes: []diff.ColumnChange{
							{Column: "bio", Before: "hello\nworld", After: "abcdefghijklmnop"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	Print(&buf, result, config.ReportConfig{MaxLinesPerOperation: 50, MaxValueLength: 8})

	got := buf.String()
	want := "       id=1 bio: hello... → abcde..."
	if !strings.Contains(got, want) {
		t.Fatalf("report missing %q\n%s", want, got)
	}
}

func TestPrintFallsBackToGenericPKLabel(t *testing.T) {
	result := diff.Result{
		Tables: []diff.TableDiff{
			{
				Table: "users",
				Updated: []diff.RowChange{
					{
						PK: "1",
						Changes: []diff.ColumnChange{
							{Column: "email", Before: "old@example.com", After: "new@example.com"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	Print(&buf, result, config.ReportConfig{MaxLinesPerOperation: 50, MaxValueLength: 200})

	got := buf.String()
	want := "       pk=1 email: old@example.com → new@example.com"
	if !strings.Contains(got, want) {
		t.Fatalf("report missing %q\n%s", want, got)
	}
}
