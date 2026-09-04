package diff

import (
	"context"
	"path/filepath"
	"testing"

	"dbtrace/internal/snapshot"
)

func TestCompareClassifiesInsertedUpdatedDeletedRows(t *testing.T) {
	dir := t.TempDir()
	beforePath := filepath.Join(dir, "before.sqlite")
	afterPath := filepath.Join(dir, "after.sqlite")

	writeSnapshot(t, beforePath, []snapshotRow{
		{
			table: "users",
			pk:    "1",
			hash:  "hash-before-1",
			values: []snapshotValue{
				{column: "id", value: "1"},
				{column: "email", value: "old@example.com"},
				{column: "last_login", value: "2025-06-02 07:26:58"},
			},
		},
		{
			table: "users",
			pk:    "2",
			hash:  "hash-delete-2",
			values: []snapshotValue{
				{column: "id", value: "2"},
				{column: "email", value: "deleted@example.com"},
			},
		},
		{
			table: "orders",
			pk:    "10",
			hash:  "same",
			values: []snapshotValue{
				{column: "id", value: "10"},
				{column: "status", value: "pending"},
			},
		},
	})

	writeSnapshot(t, afterPath, []snapshotRow{
		{
			table: "users",
			pk:    "1",
			hash:  "hash-after-1",
			values: []snapshotValue{
				{column: "id", value: "1"},
				{column: "email", value: "old@example.com"},
				{column: "last_login", value: "2025-06-15 09:22:35"},
			},
		},
		{
			table: "users",
			pk:    "3",
			hash:  "hash-insert-3",
			values: []snapshotValue{
				{column: "id", value: "3"},
				{column: "email", value: "new@example.com"},
				{column: "deleted_at", isNull: true},
			},
		},
		{
			table: "orders",
			pk:    "10",
			hash:  "same",
			values: []snapshotValue{
				{column: "id", value: "10"},
				{column: "status", value: "pending"},
			},
		},
	})

	result, err := Compare(context.Background(), beforePath, afterPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tables) != 1 {
		t.Fatalf("tables len = %d, want 1: %#v", len(result.Tables), result.Tables)
	}
	users := result.Tables[0]
	if users.Table != "users" {
		t.Fatalf("table = %q, want users", users.Table)
	}

	if len(users.Inserted) != 1 || users.Inserted[0].PK != "3" {
		t.Fatalf("inserted = %#v", users.Inserted)
	}
	if users.Inserted[0].PKColumn != "id" {
		t.Fatalf("inserted pk column = %q, want id", users.Inserted[0].PKColumn)
	}
	assertColumnChange(t, users.Inserted[0].Changes, "email", "", true, "new@example.com", false)
	assertColumnChange(t, users.Inserted[0].Changes, "deleted_at", "", true, "", true)

	if len(users.Deleted) != 1 || users.Deleted[0].PK != "2" {
		t.Fatalf("deleted = %#v", users.Deleted)
	}
	if users.Deleted[0].PKColumn != "id" {
		t.Fatalf("deleted pk column = %q, want id", users.Deleted[0].PKColumn)
	}
	assertColumnChange(t, users.Deleted[0].Changes, "email", "deleted@example.com", false, "", true)

	if len(users.Updated) != 1 || users.Updated[0].PK != "1" {
		t.Fatalf("updated = %#v", users.Updated)
	}
	if users.Updated[0].PKColumn != "id" {
		t.Fatalf("updated pk column = %q, want id", users.Updated[0].PKColumn)
	}
	if len(users.Updated[0].Changes) != 1 {
		t.Fatalf("updated changes = %#v, want only changed columns", users.Updated[0].Changes)
	}
	assertColumnChange(t, users.Updated[0].Changes, "last_login", "2025-06-02 07:26:58", false, "2025-06-15 09:22:35", false)
}

type snapshotRow struct {
	table  string
	pk     string
	hash   string
	values []snapshotValue
}

type snapshotValue struct {
	column string
	value  string
	isNull bool
}

func writeSnapshot(t *testing.T, path string, rows []snapshotRow) {
	t.Helper()

	writer, err := snapshot.OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]bool{}
	for _, row := range rows {
		if !tables[row.table] {
			if err := writer.WriteTable(row.table, "id", 0, "test"); err != nil {
				t.Fatal(err)
			}
			tables[row.table] = true
		}
		if err := writer.WriteRow(row.table, row.pk, row.hash); err != nil {
			t.Fatal(err)
		}
		for _, value := range row.values {
			if err := writer.WriteRowValue(row.table, row.pk, value.column, value.value, value.isNull); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertColumnChange(t *testing.T, changes []ColumnChange, column string, before string, beforeNull bool, after string, afterNull bool) {
	t.Helper()

	for _, change := range changes {
		if change.Column != column {
			continue
		}
		if change.Before != before || change.BeforeNull != beforeNull || change.After != after || change.AfterNull != afterNull {
			t.Fatalf("change for %s = %#v", column, change)
		}
		return
	}
	t.Fatalf("missing column change %q in %#v", column, changes)
}
