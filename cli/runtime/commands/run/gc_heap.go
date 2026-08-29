//go:build !wago_lean

package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func gcFlags() []command.Flag {
	return []command.Flag{
		{Name: "gc-heap", Arg: "<size>", Help: "throughput GC heap capacity in bytes or KiB, MiB, GiB"},
		{Name: "gc-nursery", Arg: "<size>", Help: "throughput GC nursery capacity in bytes or KiB, MiB, GiB"},
	}
}

func gcLongHelp() string {
	return "Large WasmGC workloads can set --gc-heap and --gc-nursery with binary sizes such as 2GiB and 64MiB. "
}

func instantiate(runtime *wago.Runtime, module *wago.Module, gc wago.GCConfig, configured bool) (*wago.Instance, error) {
	if configured {
		return runtime.Instantiate(context.Background(), module, wago.WithGC(gc))
	}
	return runtime.Instantiate(context.Background(), module)
}

func gcConfiguration(ctx *command.Ctx) (wago.GCConfig, bool, error) {
	var cfg wago.GCConfig
	heap := ctx.Str("gc-heap")
	if heap != "" {
		value, err := parseGCBytes(heap)
		if err != nil {
			return cfg, false, fmt.Errorf("--gc-heap: %w", err)
		}
		cfg.ThroughputHeapBytes = value
	}
	nursery := ctx.Str("gc-nursery")
	if nursery != "" {
		value, err := parseGCBytes(nursery)
		if err != nil {
			return cfg, false, fmt.Errorf("--gc-nursery: %w", err)
		}
		cfg.NurseryBytes = value
	}
	return cfg, heap != "" || nursery != "", nil
}

func parseGCBytes(raw string) (uint32, error) {
	value := raw
	multiplier := uint64(1)
	switch {
	case strings.HasSuffix(value, "GiB"):
		value, multiplier = value[:len(value)-3], 1<<30
	case strings.HasSuffix(value, "MiB"):
		value, multiplier = value[:len(value)-3], 1<<20
	case strings.HasSuffix(value, "KiB"):
		value, multiplier = value[:len(value)-3], 1<<10
	case strings.HasSuffix(value, "B"):
		value = value[:len(value)-1]
	}
	if value == "" {
		return 0, errors.New("size is empty")
	}
	limit := uint64(^uint32(0)) / multiplier
	count := uint64(0)
	for i := 0; i < len(value); i++ {
		digit := value[i]
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("invalid size %q", raw)
		}
		digit -= '0'
		if uint64(digit) > limit || count > (limit-uint64(digit))/10 {
			return 0, fmt.Errorf("size %q exceeds the 4 GiB collector address space", raw)
		}
		count = count*10 + uint64(digit)
	}
	if count == 0 {
		return 0, errors.New("size must be positive")
	}
	return uint32(count * multiplier), nil
}
