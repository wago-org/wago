package main

import (
	"strings"
	"testing"
)

func TestParseRules(t *testing.T) {
	source := []byte("# rule\nswap-chain-3 swap swap first.src=second.dst first.dst!=second.src swap-chain\n")
	rules, err := parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].name != "swap-chain-3" {
		t.Fatalf("rules = %#v", rules)
	}
	generated, err := generate("arm64", source, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func matchMachinePair", "machineRuleSwapChain3", "first.src == second.dst"} {
		if !strings.Contains(string(generated), want) {
			t.Fatalf("generated matcher does not contain %q", want)
		}
	}
}

func TestParseRulesRejectsMalformedAndDuplicateRules(t *testing.T) {
	for _, source := range []string{
		"swap-chain-3 swap swap\n",
		"copy swap swap first.src=second.dst first.dst!=second.src move\n",
		"swap-chain-3 swap swap first.src=second.dst first.dst!=second.src swap-chain\n" +
			"swap-chain-3 swap swap first.src=second.dst first.dst!=second.src swap-chain\n",
	} {
		if _, err := parse([]byte(source)); err == nil {
			t.Fatalf("parse unexpectedly accepted %q", source)
		}
	}
}
