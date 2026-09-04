package initconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesDSNConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbtrace.yaml")

	if err := Run(Options{Path: path, DSN: "user:pass@tcp(localhost:3306)/app"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, `dsn: "user:pass@tcp(localhost:3306)/app"`) {
		t.Fatalf("config missing dsn:\n%s", got)
	}
	if !strings.Contains(got, "max_value_length: 200") {
		t.Fatalf("config missing report defaults:\n%s", got)
	}
}

func TestRunRefusesOverwriteWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbtrace.yaml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Run(Options{Path: path, DSN: "user:pass@tcp(localhost:3306)/app"})
	if err == nil {
		t.Fatal("expected overwrite error")
	}
	if !strings.Contains(err.Error(), "use --force") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunForceOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbtrace.yaml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run(Options{Path: path, DSN: "user:pass@tcp(localhost:3306)/app", Force: true}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "existing" {
		t.Fatal("config was not overwritten")
	}
}

func TestDetectDSNFromEnv(t *testing.T) {
	dsn, err := detectDSN(map[string]string{
		"DB_HOST":     "127.0.0.1",
		"DB_PORT":     "3307",
		"DB_DATABASE": "app",
		"DB_USERNAME": "user",
		"DB_PASSWORD": "p@ ss",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "user:p%40%20ss@tcp(127.0.0.1:3307)/app"
	if dsn != want {
		t.Fatalf("dsn = %q, want %q", dsn, want)
	}
}

func TestDetectDSNFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	if err := os.WriteFile(".env", []byte(`
DB_CONNECTION=mysql
DB_HOST=localhost
DB_PORT=3306
DB_DATABASE=app
DB_USERNAME=user
DB_PASSWORD=secret
`), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn, err := detectDSN(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	want := "user:secret@tcp(localhost:3306)/app"
	if dsn != want {
		t.Fatalf("dsn = %q, want %q", dsn, want)
	}
}
