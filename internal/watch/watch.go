package watch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"dbtrace/internal/config"
	"dbtrace/internal/diff"
	"dbtrace/internal/report"
	"dbtrace/internal/snapshot"
)

type SnapshotFunc func(context.Context, config.Config, string, snapshot.Options) error
type DiffFunc func(context.Context, config.Config) (diff.Result, error)

type Runner struct {
	Snapshot SnapshotFunc
	Diff     DiffFunc
	In       io.Reader
	Out      io.Writer
	Verbose  bool
}

func (r Runner) Run(ctx context.Context, cfg config.Config) error {
	snapshotFn := r.Snapshot
	if snapshotFn == nil {
		return fmt.Errorf("watch snapshot function is required")
	}
	diffFn := r.Diff
	if diffFn == nil {
		return fmt.Errorf("watch diff function is required")
	}
	in := r.In
	if in == nil {
		in = os.Stdin
	}
	out := r.Out
	if out == nil {
		out = os.Stdout
	}

	fmt.Fprintln(out, "Watching database. Press Enter after performing an app action. Press Ctrl+C to stop.")
	if err := snapshotFn(ctx, cfg, "before", snapshot.Options{Verbose: r.Verbose, Out: out}); err != nil {
		return err
	}
	fmt.Fprintln(out, "Snapshot saved")

	scanner := bufio.NewScanner(in)
	action := 1
	for scanner.Scan() {
		fmt.Fprintf(out, "\nAction %d\n", action)
		if err := snapshotFn(ctx, cfg, "after", snapshot.Options{Verbose: r.Verbose, Out: out}); err != nil {
			return err
		}
		fmt.Fprintln(out, "Snapshot saved")
		fmt.Fprintln(out, "Diffing...")

		result, err := diffFn(ctx, cfg)
		if err != nil {
			return err
		}
		report.Print(out, result, cfg.Report)

		if err := RotateSnapshots(cfg); err != nil {
			return err
		}
		action++
		fmt.Fprintln(out, "\nWatching database. Press Enter after performing an app action. Press Ctrl+C to stop.")
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func RotateSnapshots(cfg config.Config) error {
	beforePath := config.SnapshotPath(cfg, "before")
	afterPath := config.SnapshotPath(cfg, "after")
	return copyFile(afterPath, beforePath)
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}
