package diff

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"dbtrace/internal/config"

	_ "github.com/ncruces/go-sqlite3/driver"
)

type Result struct {
	Tables []TableDiff
}

type TableDiff struct {
	Table    string
	Inserted []RowChange
	Updated  []RowChange
	Deleted  []RowChange
}

type RowChange struct {
	PKColumn string
	PK       string
	Changes  []ColumnChange
}

type ColumnChange struct {
	Column     string
	Before     string
	BeforeNull bool
	After      string
	AfterNull  bool
}

type rowRef struct {
	Table    string
	PKColumn string
	PK       string
}

type value struct {
	text   string
	isNull bool
}

func Run(ctx context.Context, cfg config.Config) (Result, error) {
	return Compare(ctx, config.SnapshotPath(cfg, "before"), config.SnapshotPath(cfg, "after"))
}

func Compare(ctx context.Context, beforePath string, afterPath string) (Result, error) {
	conn, err := sql.Open("sqlite3", beforePath)
	if err != nil {
		return Result{}, fmt.Errorf("open before snapshot: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS afterdb", afterPath); err != nil {
		return Result{}, fmt.Errorf("attach after snapshot: %w", err)
	}
	if err := ensurePrimaryKeyColumn(ctx, conn, "main"); err != nil {
		return Result{}, err
	}
	if err := ensurePrimaryKeyColumn(ctx, conn, "afterdb"); err != nil {
		return Result{}, err
	}

	inserted, err := queryRefs(ctx, conn, `
		SELECT a.table_name, COALESCE(NULLIF(t.primary_key, ''), 'pk'), a.pk
		FROM afterdb.rows a
		LEFT JOIN afterdb.tables t
		  ON t.table_name = a.table_name
		LEFT JOIN main.rows b
		  ON b.table_name = a.table_name AND b.pk = a.pk
		WHERE b.pk IS NULL
		ORDER BY a.table_name, a.pk
	`)
	if err != nil {
		return Result{}, fmt.Errorf("find inserted rows: %w", err)
	}

	deleted, err := queryRefs(ctx, conn, `
		SELECT b.table_name, COALESCE(NULLIF(t.primary_key, ''), 'pk'), b.pk
		FROM main.rows b
		LEFT JOIN main.tables t
		  ON t.table_name = b.table_name
		LEFT JOIN afterdb.rows a
		  ON a.table_name = b.table_name AND a.pk = b.pk
		WHERE a.pk IS NULL
		ORDER BY b.table_name, b.pk
	`)
	if err != nil {
		return Result{}, fmt.Errorf("find deleted rows: %w", err)
	}

	updated, err := queryRefs(ctx, conn, `
		SELECT b.table_name, COALESCE(NULLIF(t.primary_key, ''), 'pk'), b.pk
		FROM main.rows b
		LEFT JOIN main.tables t
		  ON t.table_name = b.table_name
		INNER JOIN afterdb.rows a
		  ON a.table_name = b.table_name AND a.pk = b.pk
		WHERE b.hash <> a.hash
		ORDER BY b.table_name, b.pk
	`)
	if err != nil {
		return Result{}, fmt.Errorf("find updated rows: %w", err)
	}

	tables := map[string]*TableDiff{}
	tableDiff := func(name string) *TableDiff {
		td, ok := tables[name]
		if !ok {
			td = &TableDiff{Table: name}
			tables[name] = td
		}
		return td
	}

	for _, ref := range inserted {
		changes, err := insertedChanges(ctx, conn, ref)
		if err != nil {
			return Result{}, err
		}
		td := tableDiff(ref.Table)
		td.Inserted = append(td.Inserted, RowChange{PKColumn: ref.PKColumn, PK: ref.PK, Changes: changes})
	}
	for _, ref := range deleted {
		changes, err := deletedChanges(ctx, conn, ref)
		if err != nil {
			return Result{}, err
		}
		td := tableDiff(ref.Table)
		td.Deleted = append(td.Deleted, RowChange{PKColumn: ref.PKColumn, PK: ref.PK, Changes: changes})
	}
	for _, ref := range updated {
		changes, err := updatedChanges(ctx, conn, ref)
		if err != nil {
			return Result{}, err
		}
		td := tableDiff(ref.Table)
		td.Updated = append(td.Updated, RowChange{PKColumn: ref.PKColumn, PK: ref.PK, Changes: changes})
	}

	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)

	result := Result{Tables: make([]TableDiff, 0, len(names))}
	for _, name := range names {
		result.Tables = append(result.Tables, *tables[name])
	}
	return result, nil
}

func ensurePrimaryKeyColumn(ctx context.Context, conn *sql.DB, schema string) error {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA %s.table_info(tables)", schema))
	if err != nil {
		return fmt.Errorf("inspect %s.tables: %w", schema, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan %s.tables info: %w", schema, err)
		}
		if name == "primary_key" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s.tables ADD COLUMN primary_key TEXT NOT NULL DEFAULT ''", schema)); err != nil {
		return fmt.Errorf("add primary_key to %s.tables: %w", schema, err)
	}
	return nil
}

func queryRefs(ctx context.Context, conn *sql.DB, query string) ([]rowRef, error) {
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []rowRef
	for rows.Next() {
		var ref rowRef
		if err := rows.Scan(&ref.Table, &ref.PKColumn, &ref.PK); err != nil {
			return nil, err
		}
		if ref.PKColumn == "" {
			ref.PKColumn = "pk"
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func insertedChanges(ctx context.Context, conn *sql.DB, ref rowRef) ([]ColumnChange, error) {
	after, err := loadValues(ctx, conn, "afterdb", ref)
	if err != nil {
		return nil, fmt.Errorf("load inserted values %s.%s: %w", ref.Table, ref.PK, err)
	}
	columns := sortedKeys(after)
	changes := make([]ColumnChange, 0, len(columns))
	for _, col := range columns {
		v := after[col]
		changes = append(changes, ColumnChange{Column: col, BeforeNull: true, After: v.text, AfterNull: v.isNull})
	}
	return changes, nil
}

func deletedChanges(ctx context.Context, conn *sql.DB, ref rowRef) ([]ColumnChange, error) {
	before, err := loadValues(ctx, conn, "main", ref)
	if err != nil {
		return nil, fmt.Errorf("load deleted values %s.%s: %w", ref.Table, ref.PK, err)
	}
	columns := sortedKeys(before)
	changes := make([]ColumnChange, 0, len(columns))
	for _, col := range columns {
		v := before[col]
		changes = append(changes, ColumnChange{Column: col, Before: v.text, BeforeNull: v.isNull, AfterNull: true})
	}
	return changes, nil
}

func updatedChanges(ctx context.Context, conn *sql.DB, ref rowRef) ([]ColumnChange, error) {
	before, err := loadValues(ctx, conn, "main", ref)
	if err != nil {
		return nil, fmt.Errorf("load before values %s.%s: %w", ref.Table, ref.PK, err)
	}
	after, err := loadValues(ctx, conn, "afterdb", ref)
	if err != nil {
		return nil, fmt.Errorf("load after values %s.%s: %w", ref.Table, ref.PK, err)
	}

	colSet := make(map[string]struct{}, len(before)+len(after))
	for col := range before {
		colSet[col] = struct{}{}
	}
	for col := range after {
		colSet[col] = struct{}{}
	}
	columns := make([]string, 0, len(colSet))
	for col := range colSet {
		columns = append(columns, col)
	}
	sort.Strings(columns)

	var changes []ColumnChange
	for _, col := range columns {
		b, bok := before[col]
		a, aok := after[col]
		if !bok {
			b.isNull = true
		}
		if !aok {
			a.isNull = true
		}
		if b.text == a.text && b.isNull == a.isNull {
			continue
		}
		changes = append(changes, ColumnChange{
			Column:     col,
			Before:     b.text,
			BeforeNull: b.isNull,
			After:      a.text,
			AfterNull:  a.isNull,
		})
	}
	return changes, nil
}

func loadValues(ctx context.Context, conn *sql.DB, schema string, ref rowRef) (map[string]value, error) {
	query := fmt.Sprintf(`
		SELECT column_name, value, is_null
		FROM %s.row_values
		WHERE table_name = ? AND pk = ?
		ORDER BY column_name
	`, schema)
	rows, err := conn.QueryContext(ctx, query, ref.Table, ref.PK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]value{}
	for rows.Next() {
		var column string
		var text sql.NullString
		var isNull int
		if err := rows.Scan(&column, &text, &isNull); err != nil {
			return nil, err
		}
		out[column] = value{text: text.String, isNull: isNull == 1}
	}
	return out, rows.Err()
}

func sortedKeys(m map[string]value) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
