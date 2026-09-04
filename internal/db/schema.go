package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"dbtrace/internal/config"
)

type Table struct {
	Name         string
	PrimaryKey   string
	KeyColumns   []string
	KeyKind      string
	SyntheticKey bool
	Columns      []Column
}

type Column struct {
	Name string
}

type Discovery struct {
	Tables   []Table
	Warnings []string
}

func Discover(ctx context.Context, conn *sql.DB, cfg config.Config) (Discovery, error) {
	tableNames, err := listTables(ctx, conn)
	if err != nil {
		return Discovery{}, err
	}

	var out Discovery
	for _, tableName := range tableNames {
		cols, err := columns(ctx, conn, tableName)
		if err != nil {
			return Discovery{}, err
		}
		pks, err := primaryKeys(ctx, conn, tableName)
		if err != nil {
			return Discovery{}, err
		}
		uniqueKeys, err := uniqueIndexes(ctx, conn, tableName)
		if err != nil {
			return Discovery{}, err
		}
		key, warning := resolveKey(tableName, cols, configuredKeysForTable(cfg.Keys, tableName), pks, uniqueKeys)
		if warning != "" {
			out.Warnings = append(out.Warnings, warning)
		}
		out.Tables = append(out.Tables, Table{
			Name:         tableName,
			PrimaryKey:   primaryKeyLabel(key.Columns),
			KeyColumns:   key.Columns,
			KeyKind:      key.Kind,
			SyntheticKey: key.Synthetic,
			Columns:      cols,
		})
	}
	return out, nil
}

type tableKey struct {
	Columns   []string
	Kind      string
	Synthetic bool
}

func resolveKey(tableName string, cols []Column, configured []string, pks []string, uniqueKeys [][]string) (tableKey, string) {
	if len(configured) > 0 {
		if missing := missingColumns(cols, configured); len(missing) > 0 {
			return tableKey{Columns: nil, Kind: "synthetic", Synthetic: true}, fmt.Sprintf("table %s configured key columns missing: %s; using synthetic row identity", tableName, strings.Join(missing, ","))
		}
		return tableKey{Columns: configured, Kind: "configured"}, ""
	}
	if len(pks) > 0 {
		return tableKey{Columns: pks, Kind: "primary"}, ""
	}
	if len(uniqueKeys) > 0 {
		sort.SliceStable(uniqueKeys, func(i, j int) bool {
			if len(uniqueKeys[i]) == len(uniqueKeys[j]) {
				return strings.Join(uniqueKeys[i], ",") < strings.Join(uniqueKeys[j], ",")
			}
			return len(uniqueKeys[i]) < len(uniqueKeys[j])
		})
		return tableKey{Columns: uniqueKeys[0], Kind: "unique"}, fmt.Sprintf("table %s has no primary key; using unique index (%s)", tableName, strings.Join(uniqueKeys[0], ","))
	}
	return tableKey{Columns: nil, Kind: "synthetic", Synthetic: true}, fmt.Sprintf("table %s has no primary key or unique index; using synthetic row identity", tableName)
}

func configuredKeysForTable(keys map[string][]string, tableName string) []string {
	for table, cols := range keys {
		if strings.EqualFold(table, tableName) {
			return cols
		}
	}
	return nil
}

func missingColumns(cols []Column, names []string) []string {
	var missing []string
	for _, name := range names {
		found := false
		for _, col := range cols {
			if strings.EqualFold(col.Name, name) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, name)
		}
	}
	return missing
}

func primaryKeyLabel(cols []string) string {
	return strings.Join(cols, ",")
}

func QuoteIdent(identifier string) (string, error) {
	if identifier == "" || strings.Contains(identifier, "\x00") {
		return "", fmt.Errorf("invalid identifier %q", identifier)
	}
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`", nil
}

func Fingerprint(ctx context.Context, conn *sql.DB, table Table) (int64, string, error) {
	qt, err := QuoteIdent(table.Name)
	if err != nil {
		return 0, "", err
	}
	hasUpdatedAt := false
	for _, col := range table.Columns {
		if strings.EqualFold(col.Name, "updated_at") {
			hasUpdatedAt = true
			break
		}
	}

	selects := "COUNT(*)"
	keyColumns := tableKeyColumns(table)
	if len(keyColumns) > 0 {
		qpk, err := QuoteIdent(keyColumns[0])
		if err != nil {
			return 0, "", err
		}
		selects += fmt.Sprintf(", COALESCE(CAST(MAX(%s) AS CHAR), '')", qpk)
	}
	if hasUpdatedAt {
		selects += ", COALESCE(CAST(MAX(`updated_at`) AS CHAR), '')"
	}

	query := fmt.Sprintf("SELECT %s FROM %s", selects, qt)
	var count int64
	var maxPK string
	var maxUpdated string
	if len(keyColumns) > 0 && hasUpdatedAt {
		if err := conn.QueryRowContext(ctx, query).Scan(&count, &maxPK, &maxUpdated); err != nil {
			return 0, "", fmt.Errorf("fingerprint %s: %w", table.Name, err)
		}
		return count, fmt.Sprintf("count=%d|max_pk=%s|max_updated_at=%s", count, maxPK, maxUpdated), nil
	}
	if len(keyColumns) > 0 {
		if err := conn.QueryRowContext(ctx, query).Scan(&count, &maxPK); err != nil {
			return 0, "", fmt.Errorf("fingerprint %s: %w", table.Name, err)
		}
		return count, fmt.Sprintf("count=%d|max_pk=%s", count, maxPK), nil
	}
	if hasUpdatedAt {
		if err := conn.QueryRowContext(ctx, query).Scan(&count, &maxUpdated); err != nil {
			return 0, "", fmt.Errorf("fingerprint %s: %w", table.Name, err)
		}
		return count, fmt.Sprintf("count=%d|max_updated_at=%s", count, maxUpdated), nil
	}
	if err := conn.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, "", fmt.Errorf("fingerprint %s: %w", table.Name, err)
	}
	return count, fmt.Sprintf("count=%d", count), nil
}

func tableKeyColumns(table Table) []string {
	if len(table.KeyColumns) > 0 {
		return table.KeyColumns
	}
	if table.PrimaryKey != "" {
		return []string{table.PrimaryKey}
	}
	return nil
}

func listTables(ctx context.Context, conn *sql.DB) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	return tables, nil
}

func primaryKeys(ctx context.Context, conn *sql.DB, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		  AND constraint_name = 'PRIMARY'
		ORDER BY ordinal_position
	`, table)
	if err != nil {
		return nil, fmt.Errorf("primary keys for %s: %w", table, err)
	}
	defer rows.Close()

	var pks []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, fmt.Errorf("scan primary key for %s: %w", table, err)
		}
		pks = append(pks, pk)
	}
	return pks, rows.Err()
}

func uniqueIndexes(ctx context.Context, conn *sql.DB, table string) ([][]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT index_name, column_name
		FROM information_schema.statistics
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		  AND non_unique = 0
		  AND index_name <> 'PRIMARY'
		ORDER BY index_name, seq_in_index
	`, table)
	if err != nil {
		return nil, fmt.Errorf("unique indexes for %s: %w", table, err)
	}
	defer rows.Close()

	indexes := map[string][]string{}
	var order []string
	for rows.Next() {
		var indexName string
		var columnName string
		if err := rows.Scan(&indexName, &columnName); err != nil {
			return nil, fmt.Errorf("scan unique index for %s: %w", table, err)
		}
		if _, ok := indexes[indexName]; !ok {
			order = append(order, indexName)
		}
		indexes[indexName] = append(indexes[indexName], columnName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([][]string, 0, len(order))
	for _, indexName := range order {
		out = append(out, indexes[indexName])
	}
	return out, nil
}

func columns(ctx context.Context, conn *sql.DB, table string) ([]Column, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		ORDER BY ordinal_position
	`, table)
	if err != nil {
		return nil, fmt.Errorf("columns for %s: %w", table, err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var col Column
		if err := rows.Scan(&col.Name); err != nil {
			return nil, fmt.Errorf("scan column for %s: %w", table, err)
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}
