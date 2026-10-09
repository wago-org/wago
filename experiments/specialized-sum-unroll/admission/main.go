//go:build amd64

// Offline compiler diagnostics; no application is executed by this command.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/frontend"
)

func main() {
	path := flag.String("module", "", "original Wasm artifact")
	out := flag.String("out", "", "diagnostic JSON")
	variant := flag.String("variant", "baseline", "baseline, D or DR (tagged research builds)")
	disabled := flag.Bool("disabled", false, "explicit existing linear-sum-loop disabled control")
	flag.Parse()
	selectVariant(*variant)
	data, err := os.ReadFile(*path)
	if err != nil {
		panic(err)
	}
	record := map[string]any{"path": *path, "input_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "variant": *variant, "disabled": *disabled}
	m, err := frontend.DecodeValidate(data)
	if err != nil {
		record["decode_validate_error"] = err.Error()
	} else {
		var stats amd64.ModuleStats
		opts := amd64.CompileOptions{Workers: 1, DeferCodeMapping: true, Stats: &stats}
		if *disabled {
			opts.Optimizations = map[string]bool{"linear-sum-loop": false}
		}
		cm, err := amd64.CompileModuleWith(m, opts)
		if err != nil {
			record["compile_error"] = err.Error()
		} else {
			base, experimental := 0, 0
			var selected []*amd64.CodegenStats
			for _, s := range stats.Funcs {
				base += s.Peephole["linear-sum-unroll4"]
				experimental += s.Peephole["experimental-linear-sum"]
				if s.Peephole["linear-sum-unroll4"]+s.Peephole["experimental-linear-sum"] > 0 {
					selected = append(selected, s)
				}
			}
			record["baseline_hits"] = base
			record["experimental_hits"] = experimental
			record["native_bytes"] = len(cm.Code)
			record["native_sha256"] = fmt.Sprintf("%x", sha256.Sum256(cm.Code))
			record["selected_functions"] = selected
			record["diagnostic_functions"] = len(stats.Funcs)
			if len(stats.Funcs) == 0 {
				panic("build with wago_codegenstats")
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
		}
	}
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(*out, append(b, '\n'), 0644); err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
