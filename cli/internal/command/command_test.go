package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
)

func TestParseAndHelpRecognition(t *testing.T) {
	leaf := &Cmd{
		Name: "leaf",
		Flags: []Flag{
			{Name: "output", Short: "o", Arg: "<file>"},
			{Name: "verbose", Short: "v", Bool: true},
		},
	}

	ctx, err := leaf.Parse("wago leaf", []string{"--output=result", "-v", "first", "--", "-not-a-flag"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := ctx.Str("output"); got != "result" || !ctx.Bool("verbose") {
		t.Fatalf("parsed flags = output %q verbose %v", got, ctx.Bool("verbose"))
	}
	if got := strings.Join(ctx.Args, ","); got != "first,-not-a-flag" {
		t.Fatalf("positionals = %q", got)
	}
	if _, err := leaf.Parse("wago leaf", []string{"-o"}); err == nil {
		t.Fatal("missing value accepted")
	}
	if _, err := leaf.Parse("wago leaf", []string{"--unknown"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if _, err := leaf.Parse("wago leaf", []string{"--verbose=yes"}); err == nil {
		t.Fatal("boolean inline value accepted")
	}

	if WantsHelp([]string{"module.wasm", "--help"}, true, leaf.Flags) {
		t.Fatal("guest --help treated as command help")
	}
	if !WantsHelp([]string{"--help", "module.wasm"}, true, leaf.Flags) ||
		WantsHelp([]string{"--", "--help"}, false, leaf.Flags) {
		t.Fatal("help recognition mismatch")
	}
	if !WantsHelp([]string{"-o", "result", "--help"}, true, leaf.Flags) {
		t.Fatal("help after a separated flag value was missed")
	}
	if WantsHelp([]string{"-o", "--help", "module.wasm"}, true, leaf.Flags) {
		t.Fatal("a value equal to --help was treated as command help")
	}

	group := &Cmd{Name: "root", Children: []*Cmd{{Name: "child", Aliases: []string{"c"}}}}
	if group.Child("child") == nil || group.Child("c") == nil || group.Child("missing") != nil {
		t.Fatal("child lookup mismatch")
	}
}

func TestDispatchSuccessfulPaths(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	var got *Ctx
	leaf := &Cmd{
		Name:  "leaf",
		Flags: []Flag{{Name: "name", Short: "n", Arg: "<name>"}},
		Run:   func(ctx *Ctx) { got = ctx },
	}
	root := &Cmd{Name: "root", Children: []*Cmd{leaf}}

	root.Dispatch("wago", nil)
	root.Dispatch("wago", []string{"--help"})
	root.Dispatch("wago", []string{"--offline", "leaf", "-n", "value", "argument"})

	if got == nil || got.Path != "wago leaf" || got.Str("name") != "value" || got.One("argument") != "argument" {
		t.Fatalf("dispatched context = %#v", got)
	}
	if got.Optional("argument") != "argument" {
		t.Fatal("optional argument mismatch")
	}
	if !automation.Offline() {
		t.Fatal("group-level automation flag was not preserved for the leaf")
	}
	if got := (&Ctx{}).Optional("argument"); got != "" {
		t.Fatalf("empty optional argument = %q", got)
	}
	if got := root.Label("wago leaf"); got != "leaf" {
		t.Fatalf("label = %q", got)
	}
}

func TestInvocationWantsHelpFollowsCommandTree(t *testing.T) {
	run := &Cmd{
		Name:        "run",
		PassThrough: true,
		Flags:       []Flag{{Name: "output", Short: "o", Arg: "<file>"}},
	}
	root := &Cmd{Name: "wago", Children: []*Cmd{run}}

	if !InvocationWantsHelp(root, nil) {
		t.Fatal("empty group invocation should show help")
	}
	if !InvocationWantsHelp(root, []string{"run", "-o", "result", "--help"}) {
		t.Fatal("nested help after a flag value was missed")
	}
	if InvocationWantsHelp(root, []string{"run", "module.wasm", "--help"}) {
		t.Fatal("guest help after pass-through was intercepted")
	}
}

func TestHelpShowsShortFormForPairedBooleanFlags(t *testing.T) {
	cmd := &Cmd{
		Name: "update",
		Flags: []Flag{
			{Name: "use", Short: "u", Bool: true, Help: "activate the result"},
			{Name: "no-use", Bool: true, Help: "leave the active result unchanged"},
		},
	}
	var output bytes.Buffer
	cmd.PrintHelp(&output, "wago update")
	if !strings.Contains(output.String(), "--<no->use, -u") {
		t.Fatalf("paired flag help omits short form:\n%s", output.String())
	}
}

func TestAutomationFlagsAndCommandSchema(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	cmd := &Cmd{
		Name: "inspect", Aliases: []string{"info"}, Summary: "inspect state", Args: "[name]",
		Automation: JSONOutput | DryRun,
		Flags:      []Flag{{Name: "global", Short: "g", Bool: true, Help: "use global state"}},
	}
	ctx, err := cmd.Parse("wago inspect", []string{"--json", "--dry-run", "--no-input", "--locked", "--offline"})
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.Bool("json") || !automation.DryRun() || !automation.NoInput() || !automation.Locked() || !automation.Offline() {
		t.Fatalf("automation flags were not applied: %#v", automation.Current())
	}

	var output bytes.Buffer
	if err := WriteSchema(&output, &Cmd{Name: "wago", Children: []*Cmd{cmd}}); err != nil {
		t.Fatal(err)
	}
	var schema CommandSchema
	if err := json.Unmarshal(output.Bytes(), &schema); err != nil {
		t.Fatalf("schema is not JSON: %v\n%s", err, output.String())
	}
	if schema.SchemaVersion != 1 || len(schema.Flags) == 0 || len(schema.Commands) != 1 || schema.Commands[0].Name != "inspect" {
		t.Fatalf("schema = %#v", schema)
	}
	flags := schema.Commands[0].Flags
	for _, name := range []string{"global", "json", "dry-run", "no-input", "locked", "offline"} {
		found := false
		for _, flag := range flags {
			found = found || flag.Name == name
		}
		if !found {
			t.Errorf("schema omits --%s: %#v", name, flags)
		}
	}
}

func TestUnsupportedAutomationOutputIsRejected(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	cmd := &Cmd{Name: "plain"}
	if _, err := cmd.Parse("wago plain", []string{"--json"}); err == nil {
		t.Fatal("unsupported --json was accepted")
	}
	if _, err := cmd.Parse("wago plain", []string{"--dry-run"}); err == nil {
		t.Fatal("unsupported --dry-run was accepted")
	}
}
