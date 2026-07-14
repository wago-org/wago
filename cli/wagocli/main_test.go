package wagocli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestUsageDocumentsCommandSurface(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	text := string(b)
	for _, want := range []string{
		"wago is a pure-Go",             // banner
		"Usage: wago",                   // usage line
		"compile and execute an export", // run
		"not implemented",               // build
		"decode and validate a module",  // validate
		"github.com/wago-org/wago",      // footer
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage text missing %q:\n%s", want, text)
		}
	}
	// Every top-level command must be listed by name (plugin was folded into pkg).
	for _, cmd := range []string{"run", "add", "rm", "plugin", "auth", "module", "env", "build", "validate", "version"} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("usage text missing command %q:\n%s", cmd, text)
		}
	}
	if strings.Contains(text, "test") {
		t.Fatalf("usage should no longer mention test:\n%s", text)
	}
}

func TestValidateModuleBytesAcceptsEmptyModule(t *testing.T) {
	// Magic + version is a valid empty WebAssembly module.
	mod := []byte{'\x00', 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}
	if err := validateModuleBytes(mod); err != nil {
		t.Fatalf("validateModuleBytes(empty module): %v", err)
	}
}

func TestValidateModuleBytesRejectsDecodeErrors(t *testing.T) {
	badMagic := []byte{'n', 'o', 'p', 'e', 0x01, 0x00, 0x00, 0x00}
	err := validateModuleBytes(badMagic)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("validateModuleBytes(bad magic) = %v, want decode error", err)
	}
}

func TestMetaAndVersionCommandConstructors(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	for _, cmd := range []*Cmd{envCommand(), buildCommand(), validateCommand(), versionCommand()} {
		if cmd == nil || cmd.Name == "" || (cmd.Run == nil && len(cmd.Children) == 0) {
			t.Fatalf("invalid command descriptor: %#v", cmd)
		}
	}
	if got := versionCommand(); len(got.Children) != 8 || got.Children[0].Name != "list" || got.Children[7].Name != "list-remote" {
		t.Fatalf("version command tree = %#v", got.Children)
	}

	dirs := wago.DirsFor(versionString())
	if err := os.MkdirAll(filepath.Dir(dirs.VersionBinary("1.2.3")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dirs.VersionBinary("1.2.3"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := setActiveVersion(dirs, "1.2.3"); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	envCommand().Run(&Ctx{})
	version := versionCommand()
	version.Children[0].Run(&Ctx{})
	version.Children[1].Run(&Ctx{})
	version.Children[2].Run(&Ctx{})
	version.Children[3].Run(&Ctx{Args: []string{"1.2.3"}})
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !strings.Contains(string(out), "WAGO_VERSION") || !strings.Contains(string(out), "WAGO_CACHE") {
		t.Fatalf("env output = %q, %v", out, err)
	}

	path := t.TempDir() + "/empty.wasm"
	if err := os.WriteFile(path, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	validateCommand().Run(&Ctx{Args: []string{path}})
}

func TestProjectBuildAndRunTargetClassification(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"version"}, false},
		{[]string{"plugin", "publish"}, false},
		{[]string{"plugin", "list"}, true},
		{[]string{"run", "x.wasm"}, true},
	} {
		if got := usesProjectBuild(tc.args); got != tc.want {
			t.Fatalf("usesProjectBuild(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
	for _, name := range []string{"module.wasm", "module.wago"} {
		if !looksLikeRunTarget(name) {
			t.Fatalf("%q not recognized as run target", name)
		}
	}
	file := t.TempDir() + "/module"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil || !looksLikeRunTarget(file) {
		t.Fatalf("existing file run target = %v", err)
	}
	if looksLikeRunTarget(t.TempDir()) || looksLikeRunTarget("not-a-command-or-file") {
		t.Fatal("directory or absent file recognized as run target")
	}
	oldColor := useColor
	t.Cleanup(func() { useColor = oldColor })
	useColor = false
	if paint("31", "x") != "x" {
		t.Fatal("disabled color paint changed")
	}
	useColor = true
	if paint("31", "x") != "\x1b[31mx\x1b[0m" {
		t.Fatal("enabled color paint changed")
	}
}

func TestUsageDoesNotAdvertiseValidateDirect(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	removedAlias := "validate" + "-direct"
	if strings.Contains(string(b), removedAlias) {
		t.Fatalf("usage should not mention removed validate alias:\n%s", b)
	}
}

func TestRunHelpCollapsesBooleanPairs(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "help-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	runCommand().printHelp(f, "wago run")
	f.Close()
	b, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(b), "--<no->st-flags") || strings.Contains(string(b), "enable: keep comparison results") {
		t.Fatalf("run help did not collapse optimization pair:\n%s", b)
	}
}

func TestCmdParseAndHelpRecognition(t *testing.T) {
	leaf := &Cmd{
		Name: "leaf",
		Flags: []Flag{
			{Name: "output", Short: "o", Arg: "<file>"},
			{Name: "verbose", Short: "v", Bool: true},
		},
	}

	ctx, err := leaf.parse("wago leaf", []string{"--output=result", "-v", "first", "--", "-not-a-flag"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := ctx.Str("output"); got != "result" || !ctx.Bool("verbose") {
		t.Fatalf("parsed flags = output %q verbose %v", got, ctx.Bool("verbose"))
	}
	if got := strings.Join(ctx.Args, ","); got != "first,-not-a-flag" {
		t.Fatalf("positionals = %q", got)
	}
	if _, err := leaf.parse("wago leaf", []string{"-o"}); err == nil {
		t.Fatal("missing value accepted")
	}
	if _, err := leaf.parse("wago leaf", []string{"--unknown"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if _, err := leaf.parse("wago leaf", []string{"--verbose=yes"}); err == nil {
		t.Fatal("boolean inline value accepted")
	}

	if wantsHelp([]string{"module.wasm", "--help"}, true) {
		t.Fatal("guest --help treated as command help")
	}
	if !wantsHelp([]string{"--help", "module.wasm"}, true) || wantsHelp([]string{"--", "--help"}, false) {
		t.Fatal("help recognition mismatch")
	}
	group := &Cmd{Name: "root", Children: []*Cmd{{Name: "child", Aliases: []string{"c"}}}}
	if group.child("child") == nil || group.child("c") == nil || group.child("missing") != nil {
		t.Fatal("child lookup mismatch")
	}
}

func TestCmdDispatchSuccessfulPaths(t *testing.T) {
	var got *Ctx
	leaf := &Cmd{Name: "leaf", Flags: []Flag{{Name: "name", Short: "n", Arg: "<name>"}}, Run: func(c *Ctx) { got = c }}
	root := &Cmd{Name: "root", Children: []*Cmd{leaf}}
	root.Dispatch("wago", nil) // group help
	root.Dispatch("wago", []string{"--help"})
	root.Dispatch("wago", []string{"leaf", "-n", "value", "argument"})
	if got == nil || got.Path != "wago leaf" || got.Str("name") != "value" || got.one("argument") != "argument" {
		t.Fatalf("dispatched context = %#v", got)
	}
	if got.opt("argument") != "argument" {
		t.Fatal("optional argument mismatch")
	}
	if got := (&Ctx{}).opt("argument"); got != "" {
		t.Fatalf("empty optional argument = %q", got)
	}
	if got := root.label("wago leaf"); got != "leaf" {
		t.Fatalf("label = %q", got)
	}
}

func TestOptimizationFlagSurfaceAndListing(t *testing.T) {
	knobs := wago.OptKnobs()
	if len(knobs) == 0 {
		t.Fatal("no optimization knobs")
	}
	flags := optKnobFlags()
	if len(flags) != len(knobs)*2 {
		t.Fatalf("optimization flags = %d, want %d", len(flags), len(knobs)*2)
	}
	for i, knob := range knobs {
		if flags[i*2].Name != knob.Name || !flags[i*2].Bool || flags[i*2+1].Name != "no-"+knob.Name || !flags[i*2+1].Bool {
			t.Fatalf("flag pair %d = %#v, %#v", i, flags[i*2], flags[i*2+1])
		}
	}
	t.Cleanup(func() {
		for _, knob := range knobs {
			wago.SetOptKnob(knob.Name, knob.On)
		}
	})
	name := knobs[0].Name
	applyOptFlags(&Ctx{bools: map[string]bool{name: true}})
	if !wago.OptKnobs()[0].On {
		t.Fatalf("--%s did not enable knob", name)
	}
	applyOptFlags(&Ctx{bools: map[string]bool{"no-" + name: true}})
	if wago.OptKnobs()[0].On {
		t.Fatalf("--no-%s did not disable knob", name)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	optsCommand().Run(&Ctx{})
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !strings.Contains(string(out), "KNOB") || !strings.Contains(string(out), name) {
		t.Fatalf("opts output = %q, %v", out, err)
	}
}
