package command

import "strings"

// Complete returns command and flag candidates for one shell completion
// request. args excludes the executable name and includes the current word as
// its final element.
func Complete(root *Cmd, args []string) []string {
	if len(args) == 0 {
		args = []string{""}
	}
	current := root
	expectsValue := false
	for _, word := range args[:len(args)-1] {
		if expectsValue {
			expectsValue = false
			continue
		}
		if word == "--" {
			return nil
		}
		if flag, separateValue := completionFlag(current, root, word); flag != nil {
			expectsValue = separateValue && !flag.Bool
			continue
		}
		if child := current.Child(word); child != nil {
			current = child
			continue
		}
		if len(current.Children) != 0 {
			return nil
		}
	}

	prefix := args[len(args)-1]
	if expectsValue {
		return nil
	}
	var candidates []string
	if prefix == "" || strings.HasPrefix(prefix, "-") {
		for _, flag := range completionFlags(current, current == root) {
			candidates = appendCandidate(candidates, "--"+flag.Name, prefix)
			if flag.Short != "" {
				candidates = appendCandidate(candidates, "-"+flag.Short, prefix)
			}
		}
	}
	if prefix == "" || !strings.HasPrefix(prefix, "-") {
		for _, child := range current.Children {
			candidates = appendCandidate(candidates, child.Name, prefix)
			for _, alias := range child.Aliases {
				candidates = appendCandidate(candidates, alias, prefix)
			}
		}
	}
	return candidates
}

func appendCandidate(candidates []string, candidate, prefix string) []string {
	if strings.HasPrefix(candidate, prefix) {
		return append(candidates, candidate)
	}
	return candidates
}

func completionFlag(cmd, root *Cmd, word string) (*Flag, bool) {
	if !strings.HasPrefix(word, "-") || word == "-" {
		return nil, false
	}
	if strings.HasPrefix(word, "--") {
		name, _, hasValue := strings.Cut(strings.TrimPrefix(word, "--"), "=")
		for _, flag := range completionFlags(cmd, cmd == root) {
			if flag.Name == name {
				return &flag, !hasValue
			}
		}
		return nil, false
	}
	short := strings.TrimPrefix(word, "-")
	if len(short) == 0 {
		return nil, false
	}
	for _, flag := range completionFlags(cmd, cmd == root) {
		if flag.Short != "" && strings.HasPrefix(short, flag.Short) {
			return &flag, len(short) == len(flag.Short)
		}
	}
	return nil, false
}

func completionFlags(cmd *Cmd, root bool) []Flag {
	if root {
		return []Flag{
			{Name: "version", Short: "v", Bool: true},
			{Name: "help", Short: "h", Bool: true},
			{Name: "json", Short: "j", Bool: true},
			{Name: "no-input", Bool: true},
			{Name: "dry-run", Bool: true},
			{Name: "locked", Bool: true},
			{Name: "offline", Bool: true},
		}
	}
	flags := append([]Flag(nil), cmd.Flags...)
	flags = append(flags, cmd.automationFlags()...)
	flags = append(flags, cmd.Knobs...)
	flags = append(flags, Flag{Name: "help", Short: "h", Bool: true})
	return flags
}
