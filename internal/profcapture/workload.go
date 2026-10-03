package profcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Call uses public ABI slots; arguments/results are never copied into a bundle.
type Call struct {
	Export string   `json:"export"`
	Args   []uint64 `json:"args"`
	Want   []uint64 `json:"want"`
}
type Workload struct {
	ID           string          `json:"id"`
	Artifact     string          `json:"artifact"`
	Hash         string          `json:"artifact_sha256"`
	Init         string          `json:"init"`
	Calls        []Call          `json:"exec"`
	Command      json.RawMessage `json:"command,omitempty"`
	SemanticExec []string        `json:"semantic_exec,omitempty"`
	semantic     []semanticCase
}

func LoadWorkload(catalog, id, module, export, init, args, want string) (Workload, []byte, error) {
	var w Workload
	if id != "" {
		if module != "" || export != "" || init != "" || args != "" || want != "" {
			return w, nil, fmt.Errorf("workload preset cannot be combined with module/export/init/args/want overrides")
		}
		b, err := os.ReadFile(catalog)
		if err != nil {
			return w, nil, err
		}
		var c struct {
			Schema     int            `json:"schema"`
			Benchmarks []Workload     `json:"benchmarks"`
			Checks     []semanticCase `json:"checks"`
		}
		if err = json.Unmarshal(b, &c); err != nil {
			return w, nil, err
		}
		if c.Schema != 1 {
			return w, nil, fmt.Errorf("unsupported corpus schema %d", c.Schema)
		}
		found := false
		for _, entry := range c.Benchmarks {
			if entry.ID == id {
				w = entry
				found = true
				break
			}
		}
		if !found {
			return w, nil, fmt.Errorf("unknown corpus workload %q", id)
		}
		if len(w.Command) > 0 || (len(w.Calls) == 0 && len(w.SemanticExec) == 0) {
			return w, nil, fmt.Errorf("workload %q needs an unsupported command/import environment", id)
		}
		if len(w.SemanticExec) != 0 {
			if len(w.Calls) != 0 {
				return w, nil, fmt.Errorf("workload cannot mix exec and semantic_exec")
			}
			for _, id := range w.SemanticExec {
				var matches []semanticCase
				for _, check := range c.Checks {
					if check.ID == id {
						matches = append(matches, check)
					}
				}
				if len(matches) != 1 {
					return w, nil, fmt.Errorf("semantic check %q needs exactly one contract", id)
				}
				check := matches[0]
				if check.Artifact != w.Artifact || check.Hash != w.Hash {
					return w, nil, fmt.Errorf("semantic check %q artifact differs from workload", id)
				}
				if err := check.validate(); err != nil {
					return w, nil, fmt.Errorf("semantic check %q: %w", id, err)
				}
				w.semantic = append(w.semantic, check)
			}
		}
		w.Artifact = filepath.Join(filepath.Dir(catalog), w.Artifact)
	} else {
		if module == "" || export == "" {
			return w, nil, fmt.Errorf("provide --workload or both --module and --export")
		}
		if want == "" {
			return w, nil, fmt.Errorf("provide --want with exact result slots (use [] for void)")
		}
		a, err := ParseSlots(args)
		if err != nil {
			return w, nil, fmt.Errorf("args: %w", err)
		}
		expected, err := ParseSlots(want)
		if err != nil {
			return w, nil, fmt.Errorf("want: %w", err)
		}
		w = Workload{ID: "custom", Artifact: module, Init: init, Calls: []Call{{Export: export, Args: a, Want: expected}}}
	}
	for _, c := range w.Calls {
		if c.Export == "" || c.Want == nil {
			return w, nil, fmt.Errorf("every call needs an export and exact result oracle")
		}
	}
	b, err := os.ReadFile(w.Artifact)
	if err != nil {
		return w, nil, err
	}
	sum := sha256.Sum256(b)
	actual := hex.EncodeToString(sum[:])
	if w.Hash != "" && w.Hash != actual {
		return w, nil, fmt.Errorf("workload hash mismatch: got %s, want %s", actual, w.Hash)
	}
	w.Hash = actual
	return w, b, nil
}
func ParseSlots(s string) ([]uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []uint64{}, nil
	}
	if strings.HasPrefix(s, "[") || strings.HasSuffix(s, "]") {
		if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
			return nil, fmt.Errorf("slot list needs matching outer brackets")
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
		if s == "" {
			return []uint64{}, nil
		}
	}
	var out []uint64
	for _, part := range strings.Split(s, ",") {
		n, err := strconv.ParseUint(strings.TrimSpace(part), 0, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
