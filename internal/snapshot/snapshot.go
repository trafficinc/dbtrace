package snapshot

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"dbtrace/internal/config"
	"dbtrace/internal/db"
	"dbtrace/internal/ignore"
)

type Options struct {
	Verbose bool
	Out     io.Writer
}

func Run(ctx context.Context, cfg config.Config, label string) error {
	return RunWithOptions(ctx, cfg, label, Options{})
}

func RunWithOptions(ctx context.Context, cfg config.Config, label string, opts Options) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintln(out, "Scanning...")

	conn, err := db.Connect(ctx, cfg.Database.DSN, cfg.Snapshot.Workers)
	if err != nil {
		return err
	}
	defer conn.Close()

	discovery, err := db.Discover(ctx, conn, cfg)
	if err != nil {
		return err
	}
	printDiscoveryWarnings(out, discovery.Warnings, opts.Verbose)

	writer, err := OpenWriter(config.SnapshotPath(cfg, label))
	if err != nil {
		return err
	}

	rules := ignore.New(cfg.Ignore)
	var tables []db.Table
	for _, table := range discovery.Tables {
		if rules.IsIgnoredTable(table.Name) {
			continue
		}
		tables = append(tables, table)
	}

	if len(tables) == 0 {
		fmt.Fprintln(out, "No tables to scan")
		fmt.Fprintln(out, "Complete.")
		return writer.Close()
	}

	jobs := make(chan db.Table)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	workers := cfg.Snapshot.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(tables) {
		workers = len(tables)
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for table := range jobs {
				if err := processTable(ctx, conn, writer, rules, cfg, table, opts, out); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
	}

	for _, table := range tables {
		select {
		case err := <-errCh:
			writer.Abort()
			close(jobs)
			wg.Wait()
			_ = writer.Close()
			return err
		case jobs <- table:
		case <-ctx.Done():
			writer.Abort()
			close(jobs)
			wg.Wait()
			_ = writer.Close()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		writer.Abort()
		_ = writer.Close()
		return err
	default:
	}

	if err := writer.Close(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Complete.")
	return nil
}

func printDiscoveryWarnings(out io.Writer, warnings []string, verbose bool) {
	if len(warnings) == 0 {
		return
	}
	if verbose {
		for _, warning := range warnings {
			fmt.Fprintln(out, "warning:", warning)
		}
		return
	}
	fmt.Fprintln(out, "Some tables used fallback row identity. Run with --verbose for details.")
}

func processTable(ctx context.Context, conn *sql.DB, writer *Writer, rules ignore.Rules, cfg config.Config, table db.Table, opts Options, out io.Writer) error {
	if opts.Verbose {
		fmt.Fprintln(out, "Scanning:", table.Name)
	}

	rowCount, fingerprint, err := db.Fingerprint(ctx, conn, table)
	if err != nil {
		return err
	}
	if err := writer.WriteTable(table.Name, tableKeyLabel(table), rowCount, fingerprint); err != nil {
		return fmt.Errorf("write table metadata for %s: %w", table.Name, err)
	}

	scanned, err := db.ScanTable(ctx, conn, table, rules, cfg.Snapshot.ChunkSize, func(row db.RowSnapshot) error {
		if err := writer.WriteRow(table.Name, row.PK, row.Hash); err != nil {
			return fmt.Errorf("write row %s.%s: %w", table.Name, row.PK, err)
		}
		for _, value := range row.Values {
			if err := writer.WriteRowValue(table.Name, row.PK, value.Column, value.Value, value.IsNull); err != nil {
				return fmt.Errorf("write row value %s.%s.%s: %w", table.Name, row.PK, value.Column, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintf(out, "Scanned: %s (%d rows)\n", table.Name, scanned)
	}
	return nil
}

func tableKeyLabel(table db.Table) string {
	if table.PrimaryKey != "" {
		return table.PrimaryKey
	}
	return strings.Join(table.KeyColumns, ",")
}
