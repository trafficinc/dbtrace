package snapshot

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"dbtrace/internal/config"
	"dbtrace/internal/db"
	"dbtrace/internal/ignore"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func TestProcessTableWritesSnapshotData(t *testing.T) {
	source, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	if _, err := source.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT,
			updated_at TEXT
		);
		INSERT INTO users(id, email, updated_at) VALUES
			(1, 'one@example.com', '2026-01-01 10:00:00'),
			(2, 'two@example.com', '2026-01-02 10:00:00');
	`); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "before.sqlite")
	writer, err := OpenWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	cfg.Snapshot.ChunkSize = 1
	rules := ignore.New(config.IgnoreConfig{Columns: []string{"updated_at"}})
	table := db.Table{
		Name:       "users",
		PrimaryKey: "id",
		Columns: []db.Column{
			{Name: "id"},
			{Name: "email"},
			{Name: "updated_at"},
		},
	}

	if err := processTable(context.Background(), source, writer, rules, cfg, table, Options{}, io.Discard); err != nil {
		writer.Abort()
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	snapshotDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotDB.Close()

	var rowCount int64
	var fingerprint string
	if err := snapshotDB.QueryRow(`
		SELECT row_count, fingerprint
		FROM tables
		WHERE table_name = 'users'
	`).Scan(&rowCount, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if rowCount != 2 {
		t.Fatalf("row count = %d, want 2", rowCount)
	}
	if fingerprint != "count=2|max_pk=2|max_updated_at=2026-01-02 10:00:00" {
		t.Fatalf("fingerprint = %q", fingerprint)
	}

	var rows int
	if err := snapshotDB.QueryRow(`SELECT COUNT(*) FROM rows WHERE table_name = 'users'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("snapshot rows = %d, want 2", rows)
	}

	var ignoredValues int
	if err := snapshotDB.QueryRow(`
		SELECT COUNT(*)
		FROM row_values
		WHERE table_name = 'users' AND column_name = 'updated_at'
	`).Scan(&ignoredValues); err != nil {
		t.Fatal(err)
	}
	if ignoredValues != 0 {
		t.Fatalf("ignored updated_at values = %d, want 0", ignoredValues)
	}

	var emailValues int
	if err := snapshotDB.QueryRow(`
		SELECT COUNT(*)
		FROM row_values
		WHERE table_name = 'users' AND column_name = 'email'
	`).Scan(&emailValues); err != nil {
		t.Fatal(err)
	}
	if emailValues != 2 {
		t.Fatalf("email row values = %d, want 2", emailValues)
	}
}

func TestProcessTableProgressOutputRespectsVerbose(t *testing.T) {
	source := openSnapshotSource(t)
	defer source.Close()

	table := db.Table{
		Name:       "users",
		PrimaryKey: "id",
		Columns:    []db.Column{{Name: "id"}, {Name: "email"}},
	}
	cfg := config.Config{}
	cfg.Snapshot.ChunkSize = 10
	rules := ignore.New(config.IgnoreConfig{})

	quietPath := filepath.Join(t.TempDir(), "quiet.sqlite")
	quietWriter, err := OpenWriter(quietPath)
	if err != nil {
		t.Fatal(err)
	}
	var quietOut bytes.Buffer
	if err := processTable(context.Background(), source, quietWriter, rules, cfg, table, Options{Out: &quietOut}, &quietOut); err != nil {
		quietWriter.Abort()
		_ = quietWriter.Close()
		t.Fatal(err)
	}
	if err := quietWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if quietOut.String() != "" {
		t.Fatalf("quiet output = %q, want empty", quietOut.String())
	}

	verbosePath := filepath.Join(t.TempDir(), "verbose.sqlite")
	verboseWriter, err := OpenWriter(verbosePath)
	if err != nil {
		t.Fatal(err)
	}
	var verboseOut bytes.Buffer
	if err := processTable(context.Background(), source, verboseWriter, rules, cfg, table, Options{Verbose: true, Out: &verboseOut}, &verboseOut); err != nil {
		verboseWriter.Abort()
		_ = verboseWriter.Close()
		t.Fatal(err)
	}
	if err := verboseWriter.Close(); err != nil {
		t.Fatal(err)
	}
	got := verboseOut.String()
	if !strings.Contains(got, "Scanning: users") || !strings.Contains(got, "Scanned: users (2 rows)") {
		t.Fatalf("verbose output missing scan lines:\n%s", got)
	}
}

func TestPrintDiscoveryWarningsRespectsVerbose(t *testing.T) {
	warnings := []string{
		"table events has no primary key; using unique index (record,event)",
		"table logs has no primary key or unique index; using synthetic row identity",
	}

	var quiet bytes.Buffer
	printDiscoveryWarnings(&quiet, warnings, false)
	quietOutput := quiet.String()
	if !strings.Contains(quietOutput, "Some tables used fallback row identity. Run with --verbose for details.") {
		t.Fatalf("quiet output missing summary:\n%s", quietOutput)
	}
	if strings.Contains(quietOutput, "table events") || strings.Contains(quietOutput, "table logs") {
		t.Fatalf("quiet output included detailed warnings:\n%s", quietOutput)
	}

	var verbose bytes.Buffer
	printDiscoveryWarnings(&verbose, warnings, true)
	verboseOutput := verbose.String()
	if !strings.Contains(verboseOutput, "warning: table events has no primary key; using unique index (record,event)") {
		t.Fatalf("verbose output missing unique index warning:\n%s", verboseOutput)
	}
	if !strings.Contains(verboseOutput, "warning: table logs has no primary key or unique index; using synthetic row identity") {
		t.Fatalf("verbose output missing synthetic identity warning:\n%s", verboseOutput)
	}

	var none bytes.Buffer
	printDiscoveryWarnings(&none, nil, false)
	if none.String() != "" {
		t.Fatalf("empty warnings output = %q, want empty", none.String())
	}
}

func openSnapshotSource(t *testing.T) *sql.DB {
	t.Helper()

	source, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT
		);
		INSERT INTO users(id, email) VALUES
			(1, 'one@example.com'),
			(2, 'two@example.com');
	`); err != nil {
		source.Close()
		t.Fatal(err)
	}
	return source
}
