package ignore

import (
	"testing"

	"dbtrace/internal/config"
)

func TestRulesAreCaseInsensitive(t *testing.T) {
	rules := New(config.IgnoreConfig{
		Tables:  []string{" Sessions "},
		Columns: []string{" Updated_At "},
	})

	if !rules.IsIgnoredTable("sessions") {
		t.Fatal("expected sessions table to be ignored")
	}
	if !rules.IsIgnoredColumn("updated_at") {
		t.Fatal("expected updated_at column to be ignored")
	}
	if rules.IsIgnoredTable("users") {
		t.Fatal("did not expect users table to be ignored")
	}
	if rules.IsIgnoredColumn("email") {
		t.Fatal("did not expect email column to be ignored")
	}
}
