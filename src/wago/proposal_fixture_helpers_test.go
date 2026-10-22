//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import (
	"strconv"
	"strings"
)

type proposalFixtureValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type proposalFixtureAction struct {
	Type   string                 `json:"type"`
	Module string                 `json:"module"`
	Field  string                 `json:"field"`
	Args   []proposalFixtureValue `json:"args"`
}

type proposalFixtureFile struct {
	Commands []struct {
		Type       string                 `json:"type"`
		Line       int                    `json:"line"`
		Filename   string                 `json:"filename"`
		ModuleType string                 `json:"module_type"`
		Name       string                 `json:"name"`
		As         string                 `json:"as"`
		Action     proposalFixtureAction  `json:"action"`
		Expected   []proposalFixtureValue `json:"expected"`
		Text       string                 `json:"text"`
	} `json:"commands"`
}

func proposalDecimalBits(value string, width int) (uint64, error) {
	if strings.HasPrefix(value, "-") {
		v, err := strconv.ParseInt(value, 10, width)
		if width == 32 {
			return uint64(uint32(v)), err
		}
		return uint64(v), err
	}
	v, err := strconv.ParseUint(value, 10, width)
	return v, err
}

func proposalNegativeFailureMatches(want string, err error) bool {
	if err == nil {
		return false
	}
	want = strings.ToLower(want)
	got := strings.ToLower(err.Error())
	if strings.HasPrefix(want, "type mismatch") {
		return strings.Contains(got, "type mismatch")
	}
	for _, prefix := range []string{"unknown function", "unknown global", "unknown tag"} {
		if strings.HasPrefix(want, prefix) {
			return strings.Contains(got, prefix)
		}
	}
	switch want {
	case "incompatible import type":
		return strings.Contains(got, "incompatible import type") ||
			strings.Contains(got, "signature mismatch") ||
			strings.Contains(got, "type mismatch") ||
			strings.Contains(got, "mutability mismatch") ||
			strings.Contains(got, "element type") && strings.Contains(got, "incompatible")
	case "unknown import":
		return strings.Contains(got, "unknown import")
	case "unknown table":
		return strings.Contains(got, "unknown table")
	case "invalid result arity":
		return strings.Contains(got, "select type arity") || strings.Contains(got, "invalid result arity")
	case "size minimum must not be greater than maximum":
		return strings.Contains(got, "invalid limits") && strings.Contains(got, "max < min")
	case "constant expression required":
		// The typed-reference corpus predates extended-constant expressions.
		// When Wago accepts the arithmetic expression under that enabled proposal,
		// the declaration is still invalid if its resulting type is incompatible.
		return strings.Contains(got, "constant expression required") || strings.Contains(got, "type mismatch")
	case "out of bounds table access":
		return strings.Contains(got, "out of bounds") && (strings.Contains(got, "table") || strings.Contains(got, "element segment"))
	case "out of bounds memory access":
		return strings.Contains(got, "out of bounds") && (strings.Contains(got, "memory") || strings.Contains(got, "data segment"))
	case "unreachable":
		return strings.Contains(got, "unreachable")
	case "indirect call type mismatch":
		return strings.Contains(got, "indirect call") && (strings.Contains(got, "wrong signature") || strings.Contains(got, "type mismatch"))
	case "undefined element", "uninitialized element":
		return strings.Contains(got, "indirect call") && strings.Contains(got, "out of bounds")
	default:
		return strings.Contains(got, want)
	}
}
