package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"
	"strings"

	"dbtrace/internal/ignore"

	"github.com/cespare/xxhash/v2"
)

type RowSnapshot struct {
	PK     string
	Hash   string
	Values []ColumnValue
}

type ColumnValue struct {
	Column string
	Value  string
	IsNull bool
}

func ScanTable(ctx context.Context, conn *sql.DB, table Table, rules ignore.Rules, chunkSize int, fn func(RowSnapshot) error) (int64, error) {
	if chunkSize <= 0 {
		chunkSize = 10000
	}

	qt, err := QuoteIdent(table.Name)
	if err != nil {
		return 0, err
	}
	keyColumns := table.KeyColumns
	if len(keyColumns) == 0 && table.PrimaryKey != "" {
		keyColumns = []string{table.PrimaryKey}
	}

	if table.SyntheticKey || len(keyColumns) == 0 {
		query := fmt.Sprintf("SELECT * FROM %s", qt)
		rows, err := conn.QueryContext(ctx, query)
		if err != nil {
			return 0, fmt.Errorf("scan %s: %w", table.Name, err)
		}
		count, _, err := scanRows(rows, nil, true, rules, fn)
		if err != nil {
			_ = rows.Close()
			return int64(count), fmt.Errorf("scan rows %s: %w", table.Name, err)
		}
		if err := rows.Close(); err != nil {
			return int64(count), fmt.Errorf("close rows %s: %w", table.Name, err)
		}
		return int64(count), nil
	}

	var scanned int64
	var lastKey []string
	for {
		orderBy, err := orderByClause(keyColumns)
		if err != nil {
			return scanned, err
		}
		query := fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT ?", qt, orderBy)
		args := []any{chunkSize}
		if len(lastKey) > 0 {
			where, whereArgs, err := keysetWhereClause(keyColumns, lastKey)
			if err != nil {
				return scanned, err
			}
			query = fmt.Sprintf("SELECT * FROM %s WHERE %s ORDER BY %s LIMIT ?", qt, where, orderBy)
			args = append(whereArgs, chunkSize)
		}

		rows, err := conn.QueryContext(ctx, query, args...)
		if err != nil {
			return scanned, fmt.Errorf("scan %s: %w", table.Name, err)
		}

		count, nextKey, err := scanRows(rows, keyColumns, false, rules, fn)
		if err != nil {
			_ = rows.Close()
			return scanned, fmt.Errorf("scan rows %s: %w", table.Name, err)
		}
		if err := rows.Close(); err != nil {
			return scanned, fmt.Errorf("close rows %s: %w", table.Name, err)
		}

		scanned += int64(count)
		if count == 0 {
			break
		}
		lastKey = nextKey
	}
	return scanned, nil
}

func scanRows(rows *sql.Rows, keyColumns []string, synthetic bool, rules ignore.Rules, fn func(RowSnapshot) error) (int, []string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return 0, nil, err
	}

	count := 0
	var lastKey []string
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return count, lastKey, err
		}

		row := RowSnapshot{}
		h := xxhash.New()
		valuesByColumn := map[string]ColumnValue{}
		for i, col := range cols {
			val := raw[i]
			isKeyColumn := containsColumn(keyColumns, col)
			if rules.IsIgnoredColumn(col) && !isKeyColumn {
				continue
			}
			cv := ColumnValue{
				Column: col,
				Value:  nullStringValue(val),
				IsNull: !val.Valid,
			}
			row.Values = append(row.Values, cv)
			valuesByColumn[col] = cv
			writeHashField(h, cv)
		}
		row.Hash = hex.EncodeToString(h.Sum(nil))
		if synthetic {
			row.PK = "row_hash=" + row.Hash
			lastKey = nil
		} else {
			identity, keyValues, err := rowIdentity(keyColumns, valuesByColumn)
			if err != nil {
				return count, lastKey, err
			}
			row.PK = identity
			lastKey = keyValues
		}
		if err := fn(row); err != nil {
			return count, lastKey, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, lastKey, err
	}
	return count, lastKey, nil
}

func orderByClause(keyColumns []string) (string, error) {
	quoted := make([]string, 0, len(keyColumns))
	for _, col := range keyColumns {
		q, err := QuoteIdent(col)
		if err != nil {
			return "", err
		}
		quoted = append(quoted, q)
	}
	return strings.Join(quoted, ", "), nil
}

func keysetWhereClause(keyColumns []string, lastKey []string) (string, []any, error) {
	if len(keyColumns) != len(lastKey) {
		return "", nil, fmt.Errorf("keyset column/value mismatch")
	}
	parts := make([]string, 0, len(keyColumns))
	args := make([]any, 0, len(keyColumns)*len(keyColumns))
	for i := range keyColumns {
		var part []string
		for j := 0; j < i; j++ {
			q, err := QuoteIdent(keyColumns[j])
			if err != nil {
				return "", nil, err
			}
			part = append(part, q+" = ?")
			args = append(args, lastKey[j])
		}
		q, err := QuoteIdent(keyColumns[i])
		if err != nil {
			return "", nil, err
		}
		part = append(part, q+" > ?")
		args = append(args, lastKey[i])
		parts = append(parts, "("+strings.Join(part, " AND ")+")")
	}
	return strings.Join(parts, " OR "), args, nil
}

func containsColumn(columns []string, column string) bool {
	for _, item := range columns {
		if strings.EqualFold(item, column) {
			return true
		}
	}
	return false
}

func rowIdentity(keyColumns []string, values map[string]ColumnValue) (string, []string, error) {
	parts := make([]string, 0, len(keyColumns))
	keyValues := make([]string, 0, len(keyColumns))
	for _, keyColumn := range keyColumns {
		value, ok := lookupColumnValue(values, keyColumn)
		if !ok {
			return "", nil, fmt.Errorf("key column %q was not found", keyColumn)
		}
		text := value.Value
		if value.IsNull {
			text = "NULL"
		}
		keyValues = append(keyValues, value.Value)
		parts = append(parts, keyColumn+"="+text)
	}
	if len(parts) == 1 {
		return keyValues[0], keyValues, nil
	}
	return strings.Join(parts, "|"), keyValues, nil
}

func lookupColumnValue(values map[string]ColumnValue, column string) (ColumnValue, bool) {
	for key, value := range values {
		if strings.EqualFold(key, column) {
			return value, true
		}
	}
	return ColumnValue{}, false
}

func writeHashField(h hash.Hash, cv ColumnValue) {
	_, _ = h.Write([]byte(strconv.Itoa(len(cv.Column))))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(cv.Column))
	_, _ = h.Write([]byte("="))
	if cv.IsNull {
		_, _ = h.Write([]byte("<NULL>"))
		return
	}
	_, _ = h.Write([]byte(strconv.Itoa(len(cv.Value))))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(cv.Value))
	_, _ = h.Write([]byte(";"))
}

func nullStringValue(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}
