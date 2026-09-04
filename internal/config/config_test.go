package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbtrace.yaml")
	if err := os.WriteFile(path, []byte("database:\n  dsn: user:pass@tcp(localhost:3306)/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Snapshot.Workers != 4 {
		t.Fatalf("workers = %d, want 4", cfg.Snapshot.Workers)
	}
	if cfg.Snapshot.ChunkSize != 10000 {
		t.Fatalf("chunk size = %d, want 10000", cfg.Snapshot.ChunkSize)
	}
	if cfg.Snapshot.OutputDir != filepath.Join(".dbtrace", "snapshots") {
		t.Fatalf("output dir = %q", cfg.Snapshot.OutputDir)
	}
}

func TestLoadRequiresDSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbtrace.yaml")
	if err := os.WriteFile(path, []byte("snapshot:\n  workers: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected missing DSN error")
	}
}

func TestSnapshotPathUsesFilepathJoin(t *testing.T) {
	cfg := Config{}
	applyDefaults(&cfg)

	got := SnapshotPath(cfg, "before")
	want := filepath.Join(".dbtrace", "snapshots", "before.sqlite")
	if got != want {
		t.Fatalf("snapshot path = %q, want %q", got, want)
	}
}
