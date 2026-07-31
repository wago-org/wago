package run

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func ParallelFlag() command.Flag {
	return command.Flag{Name: "parallel", Short: "p", Arg: "[workers]", Help: "parallel function validation and compilation; omit workers for adaptive mode"}
}

func NormalizeParallelArgs(args []string, flags []command.Flag, stopAtPositional bool) ([]string, error) {
	valueFlags := make(map[string]struct{}, len(flags)*2)
	for _, flag := range flags {
		if flag.Bool || flag.Name == "parallel" {
			continue
		}
		valueFlags["--"+flag.Name] = struct{}{}
		if flag.Short != "" {
			valueFlags["-"+flag.Short] = struct{}{}
		}
	}

	normalized := make([]string, 0, len(args)+1)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			return append(normalized, args[index:]...), nil
		}
		if arg == "-" || arg == "" || arg[0] != '-' {
			normalized = append(normalized, arg)
			if stopAtPositional {
				return append(normalized, args[index+1:]...), nil
			}
			continue
		}
		switch arg {
		case "-p", "--parallel":
			value := "auto"
			if index+1 < len(args) {
				if _, err := strconv.Atoi(args[index+1]); err == nil {
					value = args[index+1]
					index++
				}
			}
			normalized = append(normalized, "--parallel="+value)
			continue
		}
		if strings.HasPrefix(arg, "-p") && !strings.HasPrefix(arg, "-p=") && len(arg) > 2 {
			if _, err := strconv.Atoi(arg[2:]); err == nil {
				normalized = append(normalized, "--parallel="+arg[2:])
				continue
			}
		}
		name, inline := arg, false
		if equals := strings.IndexByte(arg, '='); equals >= 0 {
			name, inline = arg[:equals], true
		}
		normalized = append(normalized, arg)
		if _, ok := valueFlags[name]; ok && !inline && index+1 < len(args) {
			index++
			normalized = append(normalized, args[index])
		}
	}
	return normalized, nil
}

func Config(bounds, parallel string) (*wago.RuntimeConfig, error) {
	config := wago.NewRuntimeConfig()
	switch bounds {
	case "", "defer":
	case "all":
		config = config.WithDeferBoundsChecks(false)
	default:
		return nil, fmt.Errorf("unknown --bounds %q (want: defer, all)", bounds)
	}
	workers, err := ParallelPolicy(parallel)
	if err != nil {
		return nil, err
	}
	return config.WithFunctionWorkers(workers), nil
}

func ParallelPolicy(parallel string) (int, error) {
	switch parallel {
	case "":
		return 1, nil
	case "auto":
		return 0, nil
	default:
		workers, err := strconv.Atoi(parallel)
		if err != nil || workers < 0 {
			return 0, fmt.Errorf("invalid parallelism %q (want: -p, or a non-negative worker count)", parallel)
		}
		return workers, nil
	}
}
