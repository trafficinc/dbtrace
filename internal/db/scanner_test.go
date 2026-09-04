package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"dbtrace/internal/config"
	"dbtrace/internal/ignore"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func TestScanTableUsesKeysetAndIgnoresColumns(t *testing.T) {
	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT,
			updated_at TEXT,
			deleted_at TEXT
		);
		INSERT INTO users(id, email, updated_at, deleted_at) VALUES
			(1, 'one@example.com', 'noise-a', NULL),
			(2, 'two@example.com', 'noise-b', '2026-01-01');
	`); err != nil {
		t.Fatal(err)
	}

	rules := ignore.New(config.IgnoreConfig{Columns: []string{"updated_at"}})
	var rows []RowSnapshot
	scanned, err := ScanTable(context.Background(), conn, Table{Name: "users", PrimaryKey: "id"}, rules, 1, func(row RowSnapshot) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 2 {
		t.Fatalf("scanned = %d, want 2", scanned)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	if rows[0].PK != "1" || rows[1].PK != "2" {
		t.Fatalf("PKs = %q, %q", rows[0].PK, rows[1].PK)
	}

	for _, row := range rows {
		if hasColumn(row.Values, "updated_at") {
			t.Fatalf("updated_at should be ignored: %#v", row.Values)
		}
		if !hasColumn(row.Values, "id") {
			t.Fatalf("primary key must be included in values/hash input: %#v", row.Values)
		}
	}
	if !hasNullColumn(rows[0].Values, "deleted_at") {
		t.Fatalf("deleted_at NULL value not represented distinctly: %#v", rows[0].Values)
	}
}

func TestPrimaryKeyIsHashedEvenWhenIgnored(t *testing.T) {
	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT
		);
		INSERT INTO users(id, email) VALUES
			(1, 'same@example.com'),
			(2, 'same@example.com');
	`); err != nil {
		t.Fatal(err)
	}

	rules := ignore.New(config.IgnoreConfig{Columns: []string{"id"}})
	var rows []RowSnapshot
	_, err := ScanTable(context.Background(), conn, Table{Name: "users", PrimaryKey: "id"}, rules, 10, func(row RowSnapshot) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	if rows[0].Hash == rows[1].Hash {
		t.Fatalf("hashes should differ because primary key is always hashed: %q", rows[0].Hash)
	}
}

func TestScanTableHashIsDeterministic(t *testing.T) {
	first := scanSingleHash(t)
	second := scanSingleHash(t)
	if first != second {
		t.Fatalf("hashes differ across scans: %q != %q", first, second)
	}
}

func TestScanTableCompositeKeyIdentity(t *testing.T) {
	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE redcap_data (
			project_id INTEGER,
			record TEXT,
			field_name TEXT,
			value TEXT,
			updated_at TEXT
		);
		INSERT INTO redcap_data(project_id, record, field_name, value, updated_at) VALUES
			(1, '1001', 'first_name', 'Ada', 'noise-a'),
			(1, '1001', 'last_name', 'Lovelace', 'noise-b');
	`); err != nil {
		t.Fatal(err)
	}

	var rows []RowSnapshot
	_, err := ScanTable(context.Background(), conn, Table{
		Name:       "redcap_data",
		KeyColumns: []string{"project_id", "record", "field_name"},
	}, ignore.New(config.IgnoreConfig{Columns: []string{"updated_at"}}), 1, func(row RowSnapshot) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	want := "project_id=1|record=1001|field_name=first_name"
	if rows[0].PK != want {
		t.Fatalf("composite identity = %q, want %q", rows[0].PK, want)
	}
	if hasColumn(rows[0].Values, "updated_at") {
		t.Fatalf("ignored updated_at should not be stored: %#v", rows[0].Values)
	}
}

func TestScanTableSyntheticIdentity(t *testing.T) {
	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE legacy_log (
			message TEXT,
			updated_at TEXT
		);
		INSERT INTO legacy_log(message, updated_at) VALUES ('created', 'noise');
	`); err != nil {
		t.Fatal(err)
	}

	var rows []RowSnapshot
	_, err := ScanTable(context.Background(), conn, Table{
		Name:         "legacy_log",
		SyntheticKey: true,
	}, ignore.New(config.IgnoreConfig{Columns: []string{"updated_at"}}), 10, func(row RowSnapshot) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if !strings.HasPrefix(rows[0].PK, "row_hash=") {
		t.Fatalf("synthetic identity = %q", rows[0].PK)
	}
	if hasColumn(rows[0].Values, "updated_at") {
		t.Fatalf("ignored updated_at should not be stored: %#v", rows[0].Values)
	}
}

func scanSingleHash(t *testing.T) string {
	t.Helper()

	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT
		);
		INSERT INTO users(id, email) VALUES (1, 'same@example.com');
	`); err != nil {
		t.Fatal(err)
	}

	var hashes []string
	_, err := ScanTable(context.Background(), conn, Table{Name: "users", PrimaryKey: "id"}, ignore.New(config.IgnoreConfig{}), 10, func(row RowSnapshot) error {
		hashes = append(hashes, row.Hash)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 1 {
		t.Fatalf("hash count = %d, want 1", len(hashes))
	}
	return hashes[0]
}

func openScannerTestDB(t *testing.T) *sql.DB {
	t.Helper()

	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func hasColumn(values []ColumnValue, column string) bool {
	for _, value := range values {
		if value.Column == column {
			return true
		}
	}
	return false
}

func hasNullColumn(values []ColumnValue, column string) bool {
	for _, value := range values {
		if value.Column == column && value.IsNull {
			return true
		}
	}
	return false
}
