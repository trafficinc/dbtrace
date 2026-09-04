package watch

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dbtrace/internal/config"
	"dbtrace/internal/diff"
	"dbtrace/internal/snapshot"
)

func TestRunnerTakesBeforeThenActionReportsAndRotates(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{}
	cfg.Snapshot.OutputDir = dir
	cfg.Report.MaxLinesPerOperation = 50
	cfg.Report.MaxValueLength = 200

	var labels []string
	var out bytes.Buffer
	runner := Runner{
		In:  strings.NewReader("\n"),
		Out: &out,
		Snapshot: func(ctx context.Context, cfg config.Config, label string, opts snapshot.Options) error {
			labels = append(labels, label)
			return os.WriteFile(config.SnapshotPath(cfg, label), []byte(label), 0o644)
		},
		Diff: func(ctx context.Context, cfg config.Config) (diff.Result, error) {
			return diff.Result{Tables: []diff.TableDiff{
				{
					Table: "users",
					Updated: []diff.RowChange{
						{
							PKColumn: "id",
							PK:       "1",
							Changes: []diff.ColumnChange{
								{Column: "last_login", Before: "old", After: "new"},
							},
						},
					},
				},
			}}, nil
		},
	}

	if err := runner.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(labels, []string{"before", "after"}) {
		t.Fatalf("snapshot labels = %#v", labels)
	}

	got := out.String()
	for _, want := range []string{
		"Watching database. Press Enter after performing an app action.",
		"Action 1",
		"Snapshot saved",
		"Diffing...",
		"► Table: users",
		"id=1 last_login: old → new",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("watch output missing %q\n%s", want, got)
		}
	}

	beforeData, err := os.ReadFile(config.SnapshotPath(cfg, "before"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeData) != "after" {
		t.Fatalf("before snapshot was not rotated from after: %q", beforeData)
	}
}

func TestRotateSnapshotsCopiesAfterToBefore(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{}
	cfg.Snapshot.OutputDir = dir

	if err := os.WriteFile(filepath.Join(dir, "before.sqlite"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "after.sqlite"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RotateSnapshots(cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "before.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("before data = %q, want new", data)
	}
}
