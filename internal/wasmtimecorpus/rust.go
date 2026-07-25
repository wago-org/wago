package wasmtimecorpus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type rustToken struct {
	text       string
	start, end int
}

// RustFunctionSHA256 returns the digest of the exact `fn name ... { ... }`
// declaration and body. The lexer ignores comments and literals so braces or
// function-shaped text inside them cannot satisfy the port ledger.
func RustFunctionSHA256(source []byte, name string) (string, error) {
	tokens, err := lexRust(source)
	if err != nil {
		return "", err
	}
	var match []byte
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != "fn" || tokens[i+1].text != name {
			continue
		}
		open := -1
		for j := i + 2; j < len(tokens); j++ {
			if tokens[j].text == ";" {
				return "", fmt.Errorf("rust function %s has no body", name)
			}
			if tokens[j].text == "{" {
				open = j
				break
			}
		}
		if open < 0 {
			return "", fmt.Errorf("rust function %s has no body", name)
		}
		depth := 0
		closeIndex := -1
		for j := open; j < len(tokens); j++ {
			switch tokens[j].text {
			case "{":
				depth++
			case "}":
				depth--
				if depth == 0 {
					closeIndex = j
				}
			}
			if closeIndex >= 0 {
				break
			}
		}
		if closeIndex < 0 {
			return "", fmt.Errorf("rust function %s has an unterminated body", name)
		}
		if match != nil {
			return "", fmt.Errorf("rust function %s is declared more than once", name)
		}
		match = source[tokens[i].start:tokens[closeIndex].end]
	}
	if match == nil {
		return "", fmt.Errorf("rust function %s was not found", name)
	}
	sum := sha256.Sum256(match)
	return hex.EncodeToString(sum[:]), nil
}

func lexRust(source []byte) ([]rustToken, error) {
	var tokens []rustToken
	for i := 0; i < len(source); {
		c := source[i]
		if isRustSpace(c) {
			i++
			continue
		}
		if i+1 < len(source) && c == '/' && source[i+1] == '/' {
			i += 2
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(source) && c == '/' && source[i+1] == '*' {
			start := i
			i += 2
			depth := 1
			for i < len(source) && depth > 0 {
				if i+1 < len(source) && source[i] == '/' && source[i+1] == '*' {
					depth++
					i += 2
				} else if i+1 < len(source) && source[i] == '*' && source[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil, fmt.Errorf("unterminated Rust block comment at byte %d", start)
			}
			continue
		}
		if end, ok, err := rustLiteralEnd(source, i); ok {
			if err != nil {
				return nil, err
			}
			i = end
			continue
		}
		if isRustIdentStart(c) {
			start := i
			i++
			for i < len(source) && isRustIdentContinue(source[i]) {
				i++
			}
			tokens = append(tokens, rustToken{text: string(source[start:i]), start: start, end: i})
			continue
		}
		tokens = append(tokens, rustToken{text: string(source[i : i+1]), start: i, end: i + 1})
		i++
	}
	return tokens, nil
}

func rustLiteralEnd(source []byte, start int) (end int, ok bool, err error) {
	i := start
	if source[i] == 'b' && i+1 < len(source) && (source[i+1] == '"' || source[i+1] == '\'') {
		i++
	}
	if source[i] == 'b' && i+1 < len(source) && source[i+1] == 'r' {
		i++
	}
	if source[i] == 'r' {
		j := i + 1
		hashes := 0
		for j < len(source) && source[j] == '#' {
			hashes++
			j++
		}
		if j < len(source) && source[j] == '"' {
			j++
			for j < len(source) {
				if source[j] == '"' {
					k := j + 1
					for n := 0; n < hashes && k < len(source) && source[k] == '#'; n++ {
						k++
					}
					if k == j+1+hashes {
						return k, true, nil
					}
				}
				j++
			}
			return 0, true, fmt.Errorf("unterminated Rust raw string at byte %d", start)
		}
	}
	if source[i] != '"' && source[i] != '\'' {
		return 0, false, nil
	}
	quote := source[i]
	// A leading apostrophe followed by an identifier is normally a lifetime,
	// not a character literal. Treat it as punctuation unless a closing quote is
	// immediately plausible.
	if quote == '\'' && i+2 < len(source) && isRustIdentStart(source[i+1]) && source[i+2] != '\'' {
		return 0, false, nil
	}
	for j := i + 1; j < len(source); j++ {
		if source[j] == '\\' {
			j++
			continue
		}
		if source[j] == quote {
			return j + 1, true, nil
		}
		if source[j] == '\n' && quote == '\'' {
			break
		}
	}
	return 0, true, fmt.Errorf("unterminated Rust literal at byte %d", start)
}

func isRustSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
func isRustIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func isRustIdentContinue(c byte) bool { return isRustIdentStart(c) || c >= '0' && c <= '9' }
