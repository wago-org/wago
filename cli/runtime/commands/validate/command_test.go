package validate

import (
	"strings"
	"testing"
)

func TestModuleBytes(t *testing.T) {
	valid := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if err := ModuleBytes(valid); err != nil {
		t.Fatalf("valid empty module: %v", err)
	}
	if err := ModuleBytes([]byte("not wasm")); err == nil || !strings.Contains(err.Error(), "decode:") {
		t.Fatalf("malformed module error = %v", err)
	}
}

func TestHelpDocumentsParallelism(t *testing.T) {
	var output strings.Builder
	Command().PrintHelp(&output, "wago validate")
	text := output.String()
	if !strings.Contains(text, "--parallel, -p [workers]") ||
		!strings.Contains(text, "parallel function validation") {
		t.Fatalf("validate help did not document function parallelism:\n%s", text)
	}
}

func TestParallelFlagForms(t *testing.T) {
	cmd := Command()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "none", args: []string{"module.wasm"}},
		{name: "bare before", args: []string{"-p", "module.wasm"}, want: "auto"},
		{name: "joined before", args: []string{"-p8", "module.wasm"}, want: "8"},
		{name: "separated before", args: []string{"-p", "8", "module.wasm"}, want: "8"},
		{name: "long before", args: []string{"--parallel=4", "module.wasm"}, want: "4"},
		{name: "bare after", args: []string{"module.wasm", "-p"}, want: "auto"},
		{name: "joined after", args: []string{"module.wasm", "-p8"}, want: "8"},
		{name: "separated after", args: []string{"module.wasm", "-p", "8"}, want: "8"},
		{name: "long bare after", args: []string{"module.wasm", "--parallel"}, want: "auto"},
		{name: "long after", args: []string{"module.wasm", "--parallel=4"}, want: "4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := cmd.Normalize(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := cmd.Parse("wago validate", args)
			if err != nil {
				t.Fatal(err)
			}
			if got := ctx.Str("parallel"); got != tc.want {
				t.Fatalf("args=%v parallel=%q, want %q", tc.args, got, tc.want)
			}
			if len(ctx.Args) != 1 || ctx.Args[0] != "module.wasm" {
				t.Fatalf("args=%v positionals=%v", tc.args, ctx.Args)
			}
		})
	}

	args, err := cmd.Normalize([]string{"module.wasm", "--", "-p8"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := cmd.Parse("wago validate", args)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Str("parallel") != "" || len(ctx.Args) != 2 || ctx.Args[1] != "-p8" {
		t.Fatalf("terminator did not preserve -p8: parallel=%q args=%v", ctx.Str("parallel"), ctx.Args)
	}
}
