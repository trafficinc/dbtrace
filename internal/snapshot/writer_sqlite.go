package snapshot

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/ncruces/go-sqlite3/driver"
)

type Writer struct {
	mu            sync.Mutex
	db            *sql.DB
	tx            *sql.Tx
	tableStmt     *sql.Stmt
	rowStmt       *sql.Stmt
	rowValueStmt  *sql.Stmt
	closed        bool
	rollbackOnErr bool
}

func OpenWriter(path string) (*Writer, error) {
	cleanPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}
	if err := os.Remove(cleanPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("replace snapshot %s: %w", cleanPath, err)
	}

	db, err := sql.Open("sqlite3", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite snapshot: %w", err)
	}
	if _, err := db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;

		CREATE TABLE snapshot_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE tables (
			table_name TEXT PRIMARY KEY,
			primary_key TEXT NOT NULL DEFAULT '',
			row_count INTEGER NOT NULL DEFAULT 0,
			fingerprint TEXT
		);

		CREATE TABLE rows (
			table_name TEXT NOT NULL,
			pk TEXT NOT NULL,
			hash TEXT NOT NULL,
			PRIMARY KEY(table_name, pk)
		);

		CREATE INDEX idx_rows_table ON rows(table_name);

		CREATE TABLE row_values (
			table_name TEXT NOT NULL,
			pk TEXT NOT NULL,
			column_name TEXT NOT NULL,
			value TEXT,
			is_null INTEGER NOT NULL,
			PRIMARY KEY(table_name, pk, column_name)
		);

		CREATE INDEX idx_row_values_lookup ON row_values(table_name, pk);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create sqlite schema: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	w := &Writer{db: db, tx: tx}
	if w.tableStmt, err = tx.Prepare(`
		INSERT OR REPLACE INTO tables(table_name, primary_key, row_count, fingerprint)
		VALUES (?, ?, ?, ?)
	`); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("prepare table insert: %w", err)
	}
	if w.rowStmt, err = tx.Prepare(`
		INSERT OR REPLACE INTO rows(table_name, pk, hash)
		VALUES (?, ?, ?)
	`); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("prepare row insert: %w", err)
	}
	if w.rowValueStmt, err = tx.Prepare(`
		INSERT OR REPLACE INTO row_values(table_name, pk, column_name, value, is_null)
		VALUES (?, ?, ?, ?, ?)
	`); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("prepare row value insert: %w", err)
	}
	return w, nil
}

func (w *Writer) WriteTable(table string, primaryKey string, rowCount int64, fingerprint string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("snapshot writer is closed")
	}
	_, err := w.tableStmt.Exec(table, primaryKey, rowCount, fingerprint)
	return err
}

func (w *Writer) WriteRow(table string, pk string, hash string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("snapshot writer is closed")
	}
	_, err := w.rowStmt.Exec(table, pk, hash)
	return err
}

func (w *Writer) WriteRowValue(table string, pk string, column string, value string, isNull bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("snapshot writer is closed")
	}
	nullInt := 0
	if isNull {
		nullInt = 1
	}
	_, err := w.rowValueStmt.Exec(table, pk, column, value, nullInt)
	return err
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true

	var firstErr error
	for _, stmt := range []*sql.Stmt{w.tableStmt, w.rowStmt, w.rowValueStmt} {
		if stmt != nil {
			if err := stmt.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if w.tx != nil {
		if w.rollbackOnErr || firstErr != nil {
			if err := w.tx.Rollback(); err != nil && firstErr == nil {
				firstErr = err
			}
		} else if err := w.tx.Commit(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if w.db != nil {
		if err := w.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (w *Writer) Abort() {
	w.mu.Lock()
	w.rollbackOnErr = true
	w.mu.Unlock()
}
