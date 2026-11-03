//go:build !wago_lean && !tinygo && !wago_target_tinygo

package run

import (
	"fmt"
	"github.com/wago-org/wago/cli/internal/command"
	"testing"
)

func TestGCStage7Options(t *testing.T) {
	for _, workers := range []int{0, 1, 2, 4, 8} {
		for _, shift := range []int{0, 6, 12} {
			cmd := Command(testEnvironment{})
			args := []string{"module.wasm", "--gc-workers", fmt.Sprint(workers), "--gc-region-shift", fmt.Sprint(shift), "--gc-nursery", "1MiB"}
			if shift != 0 {
				args = append(args, "--gc-mature-evacuation")
			}
			normalized, err := cmd.Normalize(args)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := cmd.Parse("wago run", normalized)
			if err != nil {
				t.Fatal(err)
			}
			cfg, configured, err := gcConfiguration(ctx)
			if err != nil || !configured || cfg.Workers != workers || int(cfg.NurseryRegionShift) != shift || cfg.MatureEvacuation != (shift != 0) {
				t.Fatal(cfg, configured, err)
			}
		}
	}
	for _, v := range []string{"-1", "9", "18446744073709551616", "junk"} {
		if _, _, err := gcConfiguration(command.NewContext(nil, map[string]string{"gc-workers": v}, nil)); err == nil {
			t.Fatal("invalid workers", v)
		}
	}
}
