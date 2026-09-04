package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"dbtrace/internal/config"
	"dbtrace/internal/diff"
	"dbtrace/internal/initconfig"
	"dbtrace/internal/report"
	"dbtrace/internal/snapshot"
	"dbtrace/internal/watch"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		usage()
		return nil
	}

	cmd := args[1]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage()
		return nil
	}
	if !isCommand(cmd) {
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}

	if cmd == "init" {
		opts, err := parseInitOptions(args[2:])
		if err != nil {
			return err
		}
		if err := initconfig.Run(opts); err != nil {
			return err
		}
		fmt.Println("dbtrace.yaml written")
		return nil
	}

	runOpts, err := parseRunOptions(args[2:])
	if err != nil {
		return err
	}

	cfg, err := config.Load("dbtrace.yaml")
	if err != nil {
		return err
	}

	ctx := context.Background()
	switch cmd {
	case "before":
		if err := snapshot.RunWithOptions(ctx, cfg, "before", snapshot.Options{Verbose: runOpts.Verbose, Out: os.Stdout}); err != nil {
			return err
		}
		fmt.Println("Snapshot saved")
	case "after":
		if err := snapshot.RunWithOptions(ctx, cfg, "after", snapshot.Options{Verbose: runOpts.Verbose, Out: os.Stdout}); err != nil {
			return err
		}
		fmt.Println("Snapshot saved")
		fmt.Println("Diffing...")
		result, err := diff.Run(ctx, cfg)
		if err != nil {
			return err
		}
		report.Print(os.Stdout, result, cfg.Report)
	case "diff":
		result, err := diff.Run(ctx, cfg)
		if err != nil {
			return err
		}
		report.Print(os.Stdout, result, cfg.Report)
	case "watch":
		runner := watch.Runner{
			Snapshot: snapshot.RunWithOptions,
			Diff:     diff.Run,
			In:       os.Stdin,
			Out:      os.Stdout,
			Verbose:  runOpts.Verbose,
		}
		if err := runner.Run(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}

func usage() {
	fmt.Println("usage: dbtrace <before|after|diff|init|watch>")
}

func isCommand(cmd string) bool {
	switch cmd {
	case "before", "after", "diff", "init", "watch":
		return true
	default:
		return false
	}
}

func parseInitOptions(args []string) (initconfig.Options, error) {
	var opts initconfig.Options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--force":
			opts.Force = true
		case arg == "--dsn":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--dsn requires a value")
			}
			i++
			opts.DSN = args[i]
		case strings.HasPrefix(arg, "--dsn="):
			opts.DSN = strings.TrimPrefix(arg, "--dsn=")
		default:
			return opts, fmt.Errorf("unknown init option %q", arg)
		}
	}
	return opts, nil
}

type runOptions struct {
	Verbose bool
}

func parseRunOptions(args []string) (runOptions, error) {
	var opts runOptions
	for _, arg := range args {
		switch arg {
		case "--verbose":
			opts.Verbose = true
		default:
			return opts, fmt.Errorf("unknown option %q", arg)
		}
	}
	return opts, nil
}
