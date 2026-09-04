package ignore

import (
	"strings"

	"dbtrace/internal/config"
)

type Rules struct {
	tables  map[string]struct{}
	columns map[string]struct{}
}

func New(cfg config.IgnoreConfig) Rules {
	r := Rules{
		tables:  make(map[string]struct{}, len(cfg.Tables)),
		columns: make(map[string]struct{}, len(cfg.Columns)),
	}
	for _, table := range cfg.Tables {
		r.tables[norm(table)] = struct{}{}
	}
	for _, column := range cfg.Columns {
		r.columns[norm(column)] = struct{}{}
	}
	return r
}

func (r Rules) IsIgnoredTable(table string) bool {
	_, ok := r.tables[norm(table)]
	return ok
}

func (r Rules) IsIgnoredColumn(column string) bool {
	_, ok := r.columns[norm(column)]
	return ok
}

func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
