package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dbtrace/internal/config"
	"dbtrace/internal/db"
	"dbtrace/internal/diff"
	"dbtrace/internal/snapshot"

	_ "github.com/go-sql-driver/mysql"
)

func TestMySQLSnapshotDiffIntegration(t *testing.T) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		t.Skip("MYSQL_DSN not set")
	}

	ctx := context.Background()
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	table := fmt.Sprintf("dbtrace_it_%d", time.Now().UnixNano())
	quoted, err := db.QuoteIdent(table)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+quoted)

	if _, err := conn.ExecContext(ctx, "CREATE TABLE "+quoted+" (id INT PRIMARY KEY, status VARCHAR(32), note VARCHAR(255), updated_at DATETIME NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO "+quoted+" (id, status, note, updated_at) VALUES (1, 'pending', 'update me', '2026-01-01 10:00:00'), (2, 'active', 'delete me', '2026-01-01 10:00:00')"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	cfg.Database.DSN = dsn
	cfg.Snapshot.Workers = 1
	cfg.Snapshot.ChunkSize = 1
	cfg.Snapshot.OutputDir = filepath.Join(t.TempDir(), "snapshots")
	cfg.Report.MaxLinesPerOperation = 50
	cfg.Report.MaxValueLength = 200
	cfg.Ignore.Tables = []string{}
	cfg.Ignore.Columns = []string{"updated_at"}

	if err := snapshot.Run(ctx, cfg, "before"); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, "UPDATE "+quoted+" SET status = 'paid', updated_at = '2026-01-02 10:00:00' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "DELETE FROM "+quoted+" WHERE id = 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO "+quoted+" (id, status, note, updated_at) VALUES (3, 'new', 'insert me', '2026-01-02 10:00:00')"); err != nil {
		t.Fatal(err)
	}

	if err := snapshot.Run(ctx, cfg, "after"); err != nil {
		t.Fatal(err)
	}

	result, err := diff.Run(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	tableDiff := findTable(t, result, table)
	if len(tableDiff.Updated) != 1 {
		t.Fatalf("updated rows = %#v", tableDiff.Updated)
	}
	if tableDiff.Updated[0].PKColumn != "id" {
		t.Fatalf("updated pk column = %q, want id", tableDiff.Updated[0].PKColumn)
	}
	assertChange(t, tableDiff.Updated[0].Changes, "status", "pending", false, "paid", false)
	assertNoColumn(t, tableDiff.Updated[0].Changes, "updated_at")

	if len(tableDiff.Deleted) != 1 || tableDiff.Deleted[0].PK != "2" {
		t.Fatalf("deleted rows = %#v", tableDiff.Deleted)
	}
	if len(tableDiff.Inserted) != 1 || tableDiff.Inserted[0].PK != "3" {
		t.Fatalf("inserted rows = %#v", tableDiff.Inserted)
	}
	assertChange(t, tableDiff.Inserted[0].Changes, "status", "", true, "new", false)
}

func findTable(t *testing.T, result diff.Result, table string) diff.TableDiff {
	t.Helper()

	for _, item := range result.Tables {
		if item.Table == table {
			return item
		}
	}
	t.Fatalf("table %s not found in result: %#v", table, result.Tables)
	return diff.TableDiff{}
}

func assertChange(t *testing.T, changes []diff.ColumnChange, column string, before string, beforeNull bool, after string, afterNull bool) {
	t.Helper()

	for _, change := range changes {
		if change.Column != column {
			continue
		}
		if change.Before != before || change.BeforeNull != beforeNull || change.After != after || change.AfterNull != afterNull {
			t.Fatalf("change %s = %#v", column, change)
		}
		return
	}
	t.Fatalf("missing change for %s in %#v", column, changes)
}

func assertNoColumn(t *testing.T, changes []diff.ColumnChange, column string) {
	t.Helper()

	for _, change := range changes {
		if strings.EqualFold(change.Column, column) {
			t.Fatalf("unexpected change for %s in %#v", column, changes)
		}
	}
}
