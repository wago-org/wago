// Command docs-check validates repository-local links in tracked Markdown files.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type problem struct {
	path    string
	line    int
	message string
}

type link struct {
	line        int
	destination string
}

var htmlLink = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*["']([^"']+)["']`)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fatal(err)
	}
	files, err := trackedMarkdown(absRoot)
	if err != nil {
		fatal(err)
	}
	problems := checkFiles(absRoot, files)
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "%s:%d: %s\n", filepath.ToSlash(p.path), p.line, p.message)
	}
	if len(problems) != 0 {
		os.Exit(1)
	}
	fmt.Printf("docs-check: validated %d Markdown files\n", len(files))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "docs-check:", err)
	os.Exit(1)
}

func trackedMarkdown(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--", "*.md")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list tracked Markdown files: %w", err)
	}
	var files []string
	for _, name := range bytes.Split(out, []byte{0}) {
		if len(name) != 0 {
			files = append(files, string(name))
		}
	}
	sort.Strings(files)
	return files, nil
}

func checkFiles(root string, files []string) []problem {
	anchorCache := make(map[string]map[string]struct{})
	var problems []problem
	for _, name := range files {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			problems = append(problems, problem{name, 1, err.Error()})
			continue
		}
		for _, found := range extractLinks(string(contents)) {
			if p := checkLink(root, name, found, anchorCache); p != nil {
				problems = append(problems, *p)
			}
		}
	}
	sort.Slice(problems, func(i, j int) bool {
		if problems[i].path != problems[j].path {
			return problems[i].path < problems[j].path
		}
		if problems[i].line != problems[j].line {
			return problems[i].line < problems[j].line
		}
		return problems[i].message < problems[j].message
	})
	return problems
}

func checkLink(root, source string, found link, anchorCache map[string]map[string]struct{}) *problem {
	destination := strings.TrimSpace(found.destination)
	if destination == "" || strings.HasPrefix(destination, "//") {
		return nil
	}
	parsed, err := url.Parse(destination)
	if err != nil {
		return &problem{source, found.line, fmt.Sprintf("invalid link %q: %v", destination, err)}
	}
	if parsed.Scheme != "" || parsed.Host != "" || strings.HasPrefix(parsed.Path, "/") {
		return nil
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return &problem{source, found.line, fmt.Sprintf("invalid escaped path in %q", destination)}
	}
	target := source
	if path != "" {
		target = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(path))))
	}
	if target == ".." || strings.HasPrefix(target, "../") {
		return &problem{source, found.line, fmt.Sprintf("local link escapes the repository: %q", destination)}
	}
	if err := exactPath(root, target); err != nil {
		return &problem{source, found.line, fmt.Sprintf("broken local link %q: %v", destination, err)}
	}
	if parsed.Fragment == "" || !strings.EqualFold(filepath.Ext(target), ".md") {
		return nil
	}
	anchors, ok := anchorCache[target]
	if !ok {
		contents, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(target)))
		if readErr != nil {
			return &problem{source, found.line, fmt.Sprintf("read link target %q: %v", destination, readErr)}
		}
		anchors = markdownAnchors(string(contents))
		anchorCache[target] = anchors
	}
	if _, ok := anchors[parsed.Fragment]; !ok {
		return &problem{source, found.line, fmt.Sprintf("missing Markdown anchor %q in %s", parsed.Fragment, target)}
	}
	return nil
}

func exactPath(root, relative string) error {
	current := root
	for _, part := range strings.Split(filepath.Clean(filepath.FromSlash(relative)), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		matched := false
		folded := ""
		for _, entry := range entries {
			if entry.Name() == part {
				matched = true
				break
			}
			if strings.EqualFold(entry.Name(), part) {
				folded = entry.Name()
			}
		}
		if !matched {
			if folded != "" {
				return fmt.Errorf("path case mismatch: wrote %q, repository has %q", part, folded)
			}
			return os.ErrNotExist
		}
		current = filepath.Join(current, part)
	}
	return nil
}

func extractLinks(markdown string) []link {
	var links []link
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	for lineNo := 1; scanner.Scan(); lineNo++ {
		lineText := scanner.Text()
		trimmed := strings.TrimLeft(lineText, " \t")
		if char, length, ok := fence(trimmed); ok {
			if !inFence {
				inFence, fenceChar, fenceLen = true, char, length
			} else if char == fenceChar && length >= fenceLen {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		lineText = stripInlineCode(lineText)
		for _, destination := range inlineDestinations(lineText) {
			links = append(links, link{lineNo, destination})
		}
		if destination, ok := referenceDestination(lineText); ok {
			links = append(links, link{lineNo, destination})
		}
		for _, match := range htmlLink.FindAllStringSubmatch(lineText, -1) {
			links = append(links, link{lineNo, match[1]})
		}
	}
	return links
}

func fence(trimmed string) (byte, int, bool) {
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	char := trimmed[0]
	i := 1
	for i < len(trimmed) && trimmed[i] == char {
		i++
	}
	return char, i, i >= 3
}

func stripInlineCode(line string) string {
	result := []byte(line)
	for i := 0; i < len(result); {
		if result[i] != '`' {
			i++
			continue
		}
		start := i
		for i < len(result) && result[i] == '`' {
			i++
		}
		ticks := i - start
		end := bytes.Index(result[i:], bytes.Repeat([]byte{'`'}, ticks))
		if end < 0 {
			break
		}
		end += i + ticks
		for j := start; j < end; j++ {
			result[j] = ' '
		}
		i = end
	}
	return string(result)
}

func inlineDestinations(line string) []string {
	var destinations []string
	for offset := 0; offset < len(line); {
		rel := strings.Index(line[offset:], "](")
		if rel < 0 {
			break
		}
		start := offset + rel + 2
		for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
			start++
		}
		if start >= len(line) {
			break
		}
		if line[start] == '<' {
			end := strings.IndexByte(line[start+1:], '>')
			if end >= 0 {
				destinations = append(destinations, line[start+1:start+1+end])
				offset = start + end + 2
				continue
			}
		}
		end := start
		escaped := false
		for end < len(line) {
			char := line[end]
			if !escaped && (char == ')' || char == ' ' || char == '\t') {
				break
			}
			if char == '\\' && !escaped {
				escaped = true
			} else {
				escaped = false
			}
			end++
		}
		if end > start {
			destinations = append(destinations, strings.ReplaceAll(line[start:end], `\)`, `)`))
		}
		offset = end + 1
	}
	return destinations
}

func referenceDestination(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	marker := strings.Index(trimmed, "]:")
	if marker < 1 {
		return "", false
	}
	destination := strings.TrimSpace(trimmed[marker+2:])
	if strings.HasPrefix(destination, "<") {
		if end := strings.IndexByte(destination[1:], '>'); end >= 0 {
			return destination[1 : end+1], true
		}
	}
	if fields := strings.Fields(destination); len(fields) != 0 {
		return fields[0], true
	}
	return "", false
}

func markdownAnchors(markdown string) map[string]struct{} {
	anchors := make(map[string]struct{})
	counts := make(map[string]int)
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if char, length, ok := fence(trimmed); ok {
			if !inFence {
				inFence, fenceChar, fenceLen = true, char, length
			} else if char == fenceChar && length >= fenceLen {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		heading, ok := atxHeading(trimmed)
		if !ok {
			continue
		}
		base := githubSlug(heading)
		anchor := base
		if count := counts[base]; count != 0 {
			anchor = fmt.Sprintf("%s-%d", base, count)
		}
		counts[base]++
		anchors[anchor] = struct{}{}
	}
	return anchors
}

func atxHeading(trimmed string) (string, bool) {
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i == len(trimmed) || (trimmed[i] != ' ' && trimmed[i] != '\t') {
		return "", false
	}
	heading := strings.TrimSpace(trimmed[i:])
	heading = strings.TrimSpace(strings.TrimRight(heading, "#"))
	return heading, true
}

func githubSlug(heading string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '-', unicode.IsSpace(r):
			if unicode.IsSpace(r) {
				slug.WriteByte('-')
			} else {
				slug.WriteRune(r)
			}
		}
	}
	return slug.String()
}
