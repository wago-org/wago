// Command staticcheck-baseline rejects findings that are not in the reviewed
// Staticcheck baseline for the standard and runtime-tagged build profiles.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const baselinePath = ".just/staticcheck-baseline.tsv"

type profile struct {
	name string
	args []string
}

var profiles = []profile{
	{name: "standard", args: []string{"./..."}},
	{name: "runtime", args: []string{"-tags", "wago_runtime", "./cli/..."}},
}

type position struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type diagnostic struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location position `json:"location"`
}

type finding struct {
	Profile string
	File    string
	Code    string
	Message string
	Line    int
}

func main() {
	command := "verify"
	if len(os.Args) == 2 {
		command = os.Args[1]
	} else if len(os.Args) != 1 {
		fail(errors.New("usage: staticcheck-baseline [verify|snapshot]"))
	}
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	var findings []finding
	for _, selected := range profiles {
		current, err := runProfile(root, selected)
		if err != nil {
			fail(err)
		}
		findings = append(findings, current...)
	}
	sortFindings(findings)
	if command == "snapshot" {
		if err := writeSnapshot(baselinePath, findings); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %d reviewed-profile findings to %s\n", len(findings), baselinePath)
		return
	}
	if command != "verify" {
		fail(errors.New("usage: staticcheck-baseline [verify|snapshot]"))
	}
	baseline, err := readBaseline(baselinePath)
	if err != nil {
		fail(err)
	}
	if newFindings := findingsBeyondBaseline(findings, baseline); len(newFindings) != 0 {
		for _, item := range newFindings {
			fmt.Fprintf(os.Stderr, "new Staticcheck finding: %s:%d: %s (%s)\n", item.File, item.Line, item.Message, item.Code)
		}
		fail(fmt.Errorf("staticcheck reported %d finding(s) outside the reviewed baseline", len(newFindings)))
	}
	for _, selected := range profiles {
		count := 0
		for _, item := range findings {
			if item.Profile == selected.name {
				count++
			}
		}
		fmt.Printf("Staticcheck %s: %d existing finding(s), 0 new\n", selected.name, count)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "staticcheck-baseline:", err)
	os.Exit(1)
}

func runProfile(root string, selected profile) ([]finding, error) {
	args := append([]string{"-f", "json", "-fail", "all"}, selected.args...)
	cmd := exec.Command("staticcheck", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if stderr.Len() != 0 {
		return nil, fmt.Errorf("staticcheck profile %s wrote to stderr: %s", selected.name, strings.TrimSpace(stderr.String()))
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("run staticcheck profile %s: %w", selected.name, runErr)
		}
	}
	findings, err := parseFindings(&stdout, root, selected.name)
	if err != nil {
		return nil, fmt.Errorf("decode Staticcheck profile %s: %w", selected.name, err)
	}
	if runErr != nil && len(findings) == 0 {
		return nil, fmt.Errorf("staticcheck profile %s failed without diagnostics: %s", selected.name, strings.TrimSpace(stderr.String()))
	}
	return findings, nil
}

func parseFindings(input io.Reader, root, profileName string) ([]finding, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var findings []finding
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var item diagnostic
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("invalid JSON diagnostic %q: %w", line, err)
		}
		if item.Code == "" || item.Message == "" || item.Location.File == "" {
			return nil, fmt.Errorf("incomplete JSON diagnostic %q", line)
		}
		file := item.Location.File
		if filepath.IsAbs(file) {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				return nil, err
			}
			file = rel
		}
		findings = append(findings, finding{Profile: profileName, File: filepath.ToSlash(file), Code: item.Code, Message: item.Message, Line: item.Location.Line})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return findings, nil
}

func findingKey(item finding) string {
	return item.Profile + "\t" + item.File + "\t" + item.Code + "\t" + item.Message
}

func findingsBeyondBaseline(actual, baseline []finding) []finding {
	allowed := make(map[string]int, len(baseline))
	for _, item := range baseline {
		allowed[findingKey(item)]++
	}
	seen := make(map[string]int, len(actual))
	var added []finding
	for _, item := range actual {
		key := findingKey(item)
		seen[key]++
		if seen[key] > allowed[key] {
			added = append(added, item)
		}
	}
	return added
}

func readBaseline(path string) ([]finding, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read reviewed Staticcheck baseline: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var baseline []finding
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 || fields[0] == "" || fields[1] == "" || fields[2] == "" || fields[3] == "" {
			return nil, fmt.Errorf("invalid Staticcheck baseline line %d", lineNo)
		}
		baseline = append(baseline, finding{Profile: fields[0], File: fields[1], Code: fields[2], Message: fields[3]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return baseline, nil
}

func writeSnapshot(path string, findings []finding) error {
	var data strings.Builder
	data.WriteString("# Reviewed existing Staticcheck findings. New findings fail CI.\n")
	data.WriteString("# Columns: profile, file, check, message.\n")
	for _, item := range findings {
		fmt.Fprintf(&data, "%s\t%s\t%s\t%s\n", item.Profile, item.File, item.Code, item.Message)
	}
	return os.WriteFile(filepath.Clean(path), []byte(data.String()), 0o644)
}

func sortFindings(findings []finding) {
	sort.Slice(findings, func(i, j int) bool { return findingKey(findings[i]) < findingKey(findings[j]) })
}
