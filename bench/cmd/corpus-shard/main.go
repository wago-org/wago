// Command corpus-shard plans native correctness-corpus work and records results.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/bench/internal/corpusplan"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corpus-shard:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: corpus-shard plan | complete")
	}
	switch args[0] {
	case "plan":
		return plan(args[1:])
	case "complete":
		return complete(args[1:])
	default:
		return errors.New("usage: corpus-shard plan | complete")
	}
}

func plan(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	root := flags.String("root", "..", "repository root")
	platform := flags.String("platform", "", "tested GOOS/GOARCH target")
	shard := flags.String("shard", "", "zero-based index/count")
	sourceSHA := flags.String("source-sha", os.Getenv("CI_SOURCE_SHA"), "tested source commit")
	reportPath := flags.String("report", "", "report path")
	outputPath := flags.String("output", os.Getenv("GITHUB_OUTPUT"), "GitHub step output file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *platform == "" || *shard == "" || *sourceSHA == "" || *reportPath == "" {
		return errors.New("plan requires --platform, --shard, --source-sha, and --report")
	}
	index, count, err := corpusplan.ParseShard(*shard)
	if err != nil {
		return err
	}
	workloads, err := corpusplan.LoadCatalog(filepath.Join(*root, "corpus", "catalog.json"))
	if err != nil {
		return err
	}
	groups, err := corpusplan.Groups(workloads, count)
	if err != nil {
		return err
	}
	report := corpusplan.NewReport(*sourceSHA, *platform, index, count, groups[index])
	if err := writeReport(*reportPath, report); err != nil {
		return err
	}
	line := "selector=" + report.Selector + "\n"
	if *outputPath == "" {
		_, err = os.Stdout.WriteString(line)
	} else {
		file, openErr := os.OpenFile(filepath.Clean(*outputPath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if openErr != nil {
			return fmt.Errorf("open GitHub output: %w", openErr)
		}
		_, err = file.WriteString(line)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return fmt.Errorf("write shard selector: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s shard %d/%d planned %d workloads\n", *platform, index, count, len(report.Workloads))
	return nil
}

func complete(args []string) error {
	flags := flag.NewFlagSet("complete", flag.ContinueOnError)
	reportPath := flags.String("report", "", "report path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *reportPath == "" {
		return errors.New("complete requires --report")
	}
	data, err := os.ReadFile(*reportPath)
	if err != nil {
		return fmt.Errorf("read shard report: %w", err)
	}
	var report corpusplan.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode shard report: %w", err)
	}
	if report.Schema != 1 || report.SourceSHA == "" || report.Platform == "" || len(report.Workloads) == 0 {
		return errors.New("shard report is incomplete")
	}
	if report.Passed {
		return errors.New("shard report is already complete")
	}
	report.Passed = true
	return writeReport(*reportPath, report)
}

func writeReport(path string, report corpusplan.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode shard report: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write shard report: %w", err)
	}
	return nil
}
