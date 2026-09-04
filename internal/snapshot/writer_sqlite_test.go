package snapshot

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func TestWriterCreatesSnapshotAndStoresRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "before.sqlite")

	writer, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteTable("users", "id", 1, "count=1|max_pk=1"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRow("users", "1", "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRowValue("users", "1", "email", "dev@example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRowValue("users", "1", "deleted_at", "", true); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var tableName string
	var primaryKey string
	var rowCount int64
	var fingerprint string
	if err := conn.QueryRow(`SELECT table_name, primary_key, row_count, fingerprint FROM tables`).Scan(&tableName, &primaryKey, &rowCount, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if tableName != "users" || primaryKey != "id" || rowCount != 1 || fingerprint != "count=1|max_pk=1" {
		t.Fatalf("table metadata = %q, %q, %d, %q", tableName, primaryKey, rowCount, fingerprint)
	}

	var hash string
	if err := conn.QueryRow(`SELECT hash FROM rows WHERE table_name = ? AND pk = ?`, "users", "1").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != "abc123" {
		t.Fatalf("hash = %q", hash)
	}

	var value string
	var isNull int
	if err := conn.QueryRow(`
		SELECT value, is_null
		FROM row_values
		WHERE table_name = ? AND pk = ? AND column_name = ?
	`, "users", "1", "email").Scan(&value, &isNull); err != nil {
		t.Fatal(err)
	}
	if value != "dev@example.com" || isNull != 0 {
		t.Fatalf("email row value = %q, %d", value, isNull)
	}

	if err := conn.QueryRow(`
		SELECT value, is_null
		FROM row_values
		WHERE table_name = ? AND pk = ? AND column_name = ?
	`, "users", "1", "deleted_at").Scan(&value, &isNull); err != nil {
		t.Fatal(err)
	}
	if value != "" || isNull != 1 {
		t.Fatalf("deleted_at row value = %q, %d", value, isNull)
	}
}

func TestOpenWriterReplacesExistingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before.sqlite")
	if err := os.WriteFile(path, []byte("not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}

	writer, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM tables`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("table count = %d, want 0", count)
	}
}

func TestWriterPreservesFullColumnValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before.sqlite")
	longValue := "abcdefghijklmnopqrstuvwxyz"

	writer, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRow("users", "1", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteRowValue("users", "1", "bio", longValue, false); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var got string
	if err := conn.QueryRow(`
		SELECT value
		FROM row_values
		WHERE table_name = ? AND pk = ? AND column_name = ?
	`, "users", "1", "bio").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != longValue {
		t.Fatalf("stored value = %q, want full value %q", got, longValue)
	}
}
