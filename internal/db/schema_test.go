package db

import (
	"context"
	"strings"
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		want       string
		wantErr    bool
	}{
		{name: "simple", identifier: "users", want: "`users`"},
		{name: "embedded backtick", identifier: "we`ird", want: "`we``ird`"},
		{name: "empty", identifier: "", wantErr: true},
		{name: "nul", identifier: "bad\x00name", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QuoteIdent(tt.identifier)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("QuoteIdent(%q) = %q, want %q", tt.identifier, got, tt.want)
			}
		})
	}
}

func TestResolveKeyPriority(t *testing.T) {
	cols := []Column{{Name: "id"}, {Name: "record"}, {Name: "field_name"}, {Name: "email"}}

	key, warning := resolveKey("redcap_data", cols, []string{"record", "field_name"}, []string{"id"}, nil)
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if key.Kind != "configured" || strings.Join(key.Columns, ",") != "record,field_name" {
		t.Fatalf("configured key = %#v", key)
	}

	key, warning = resolveKey("line_items", cols, nil, []string{"record", "field_name"}, nil)
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if key.Kind != "primary" || strings.Join(key.Columns, ",") != "record,field_name" {
		t.Fatalf("primary key = %#v", key)
	}

	key, warning = resolveKey("users", cols, nil, nil, [][]string{{"email"}, {"record", "field_name"}})
	if !strings.Contains(warning, "using unique index") {
		t.Fatalf("warning = %q", warning)
	}
	if key.Kind != "unique" || strings.Join(key.Columns, ",") != "email" {
		t.Fatalf("unique key = %#v", key)
	}

	key, warning = resolveKey("events", cols, nil, nil, nil)
	if !strings.Contains(warning, "using synthetic row identity") {
		t.Fatalf("warning = %q", warning)
	}
	if !key.Synthetic || key.Kind != "synthetic" {
		t.Fatalf("synthetic key = %#v", key)
	}
}

func TestFingerprintWithoutUpdatedAt(t *testing.T) {
	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT
		);
		INSERT INTO users(id, email) VALUES
			(1, 'one@example.com'),
			(3, 'three@example.com');
	`); err != nil {
		t.Fatal(err)
	}

	count, fingerprint, err := Fingerprint(context.Background(), conn, Table{
		Name:       "users",
		PrimaryKey: "id",
		Columns:    []Column{{Name: "id"}, {Name: "email"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if fingerprint != "count=2|max_pk=3" {
		t.Fatalf("fingerprint = %q", fingerprint)
	}
}

func TestFingerprintWithUpdatedAt(t *testing.T) {
	conn := openScannerTestDB(t)
	defer conn.Close()

	if _, err := conn.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT,
			updated_at TEXT
		);
		INSERT INTO users(id, email, updated_at) VALUES
			(1, 'one@example.com', '2026-01-01 10:00:00'),
			(3, 'three@example.com', '2026-01-02 10:00:00');
	`); err != nil {
		t.Fatal(err)
	}

	count, fingerprint, err := Fingerprint(context.Background(), conn, Table{
		Name:       "users",
		PrimaryKey: "id",
		Columns:    []Column{{Name: "id"}, {Name: "email"}, {Name: "updated_at"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if fingerprint != "count=2|max_pk=3|max_updated_at=2026-01-02 10:00:00" {
		t.Fatalf("fingerprint = %q", fingerprint)
	}
}
