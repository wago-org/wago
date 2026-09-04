//go:build (linux || darwin || windows) && (amd64 || arm64)

// Command enginefuzz-worker is the persistent Railshot side of the Starshine
// engine-state differential lane. It accepts one JSON request per input line
// and writes one JSON response per output line.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/enginefuzz/oracle"
)

const maxRequestBytes = 64 << 10

type request struct {
	ID            uint64   `json:"id"`
	Path          string   `json:"path"`
	SupportPath   string   `json:"support_path,omitempty"`
	SupportPaths  []string `json:"support_paths,omitempty"`
	CaseSeed      string   `json:"case_seed"`
	Profile       string   `json:"profile"`
	OutcomeKind   string   `json:"outcome_kind"`
	FailureFamily string   `json:"failure_family,omitempty"`
	IncludeEvents bool     `json:"include_events,omitempty"`
}

type response struct {
	ID         uint64          `json:"id"`
	Status     string          `json:"status"`
	Hash       string          `json:"hash,omitempty"`
	EventCount int             `json:"event_count,omitempty"`
	Events     json.RawMessage `json:"events,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type worker struct {
	runtime *wago.Runtime
	harness *oracle.Harness
}

func newWorker() (*worker, error) {
	harness := oracle.NewHarness()
	plugins, err := harness.PluginSet()
	if err != nil {
		return nil, err
	}
	config := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithMemoryLimitPages(2)
	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config))
	if err := runtime.LoadPlugins(context.Background(), plugins); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	return &worker{runtime: runtime, harness: harness}, nil
}

func parseSeed(value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("case_seed is required")
	}
	seed, err := strconv.ParseUint(value, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid case_seed %q: %w", value, err)
	}
	return seed, nil
}

func (w *worker) run(req request) (result response) {
	result = response{ID: req.ID, Status: "error"}
	seed, err := parseSeed(req.CaseSeed)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	wasmBytes, err := os.ReadFile(req.Path)
	if err != nil {
		result.Error = fmt.Sprintf("read module: %v", err)
		return result
	}
	module, err := w.runtime.Compile(wasmBytes)
	if req.OutcomeKind == "compile-failure" {
		observation, observeErr := oracle.ObserveCompileFailure(err, req.FailureFamily)
		if module != nil {
			_ = module.Close()
		}
		if observeErr != nil {
			result.Error = fmt.Sprintf("observe compile failure: %v", observeErr)
			return result
		}
		result.Status = "complete"
		result.Hash = observation.Hash
		result.EventCount = len(observation.Events)
		if req.IncludeEvents {
			result.Events = json.RawMessage(observation.JSON)
		}
		return result
	}
	if err != nil {
		result.Error = fmt.Sprintf("compile module: %v", err)
		return result
	}
	defer func() {
		if err := module.Close(); err != nil && result.Error == "" {
			result = response{ID: req.ID, Status: "error", Error: fmt.Sprintf("close module: %v", err)}
		}
	}()

	instantiateOptions := []wago.InstantiateOption{}
	supportPaths := req.SupportPaths
	if len(supportPaths) == 0 && req.SupportPath != "" {
		supportPaths = []string{req.SupportPath}
	}
	var previousSupport *wago.Instance
	for supportIndex, supportPath := range supportPaths {
		supportBytes, err := os.ReadFile(supportPath)
		if err != nil {
			result.Error = fmt.Sprintf("read support module %d: %v", supportIndex, err)
			return result
		}
		supportModule, err := w.runtime.Compile(supportBytes)
		if err != nil {
			result.Error = fmt.Sprintf("compile support module %d: %v", supportIndex, err)
			return result
		}
		defer func() {
			if err := supportModule.Close(); err != nil && result.Error == "" {
				result = response{ID: req.ID, Status: "error", Error: fmt.Sprintf("close support module: %v", err)}
			}
		}()
		supportOptions := []wago.InstantiateOption{}
		if previousSupport != nil {
			linkedMemory, linkErr := previousSupport.ExportedMemory("state_memory")
			if linkErr != nil {
				result.Error = fmt.Sprintf("link support memory %d: %v", supportIndex, linkErr)
				return result
			}
			linkedTable, linkErr := previousSupport.ExportedTable("state_table")
			if linkErr != nil {
				result.Error = fmt.Sprintf("link support table %d: %v", supportIndex, linkErr)
				return result
			}
			linkedGlobal, linkErr := previousSupport.ExportedGlobalObject("state_global_i32")
			if linkErr != nil {
				result.Error = fmt.Sprintf("link support global %d: %v", supportIndex, linkErr)
				return result
			}
			supportOptions = append(supportOptions,
				wago.WithImport("__link", "state_memory", linkedMemory),
				wago.WithImport("__link", "state_table", linkedTable),
				wago.WithImport("__link", "state_global_i32", linkedGlobal),
			)
		}
		supportInstance, err := w.runtime.Instantiate(context.Background(), supportModule, supportOptions...)
		if err != nil {
			result.Error = fmt.Sprintf("instantiate support module %d: %v", supportIndex, err)
			return result
		}
		defer func() {
			if err := supportInstance.Close(); err != nil && result.Error == "" {
				result = response{ID: req.ID, Status: "error", Error: fmt.Sprintf("close support instance: %v", err)}
			}
		}()
		previousSupport = supportInstance
	}
	if previousSupport != nil {
		linkedMemory, err := previousSupport.ExportedMemory("state_memory")
		if err != nil {
			result.Error = fmt.Sprintf("link consumer memory: %v", err)
			return result
		}
		linkedTable, err := previousSupport.ExportedTable("state_table")
		if err != nil {
			result.Error = fmt.Sprintf("link consumer table: %v", err)
			return result
		}
		linkedGlobal, err := previousSupport.ExportedGlobalObject("state_global_i32")
		if err != nil {
			result.Error = fmt.Sprintf("link consumer global: %v", err)
			return result
		}
		// These handles are owned by the final support instance. The consumer
		// releases its import attachments when it closes; the support chain then
		// releases the actual resources. Calling Close on an instance-owned handle is
		// an API error.
		instantiateOptions = append(instantiateOptions,
			wago.WithImport("__link", "state_memory", linkedMemory),
			wago.WithImport("__link", "state_table", linkedTable),
			wago.WithImport("__link", "state_global_i32", linkedGlobal),
		)
	}

	caseState, err := w.harness.Begin(seed, req.FailureFamily)
	if err != nil {
		result.Error = fmt.Sprintf("begin case: %v", err)
		return result
	}
	instantiateOptions = append(instantiateOptions, caseState.InstantiateOptions()...)
	instance, executionErr := w.runtime.Instantiate(context.Background(), module, instantiateOptions...)
	var observation oracle.Observation
	var observeErr error
	if req.OutcomeKind == "instantiation-failure" {
		observation, observeErr = caseState.FinishInstantiationFailure(executionErr)
	} else {
		observation, observeErr = caseState.Finish(module.Metadata(), instance, executionErr)
	}
	var closeErr error
	if instance != nil {
		closeErr = instance.Close()
	}
	closeErr = errors.Join(closeErr, caseState.Close())
	if observeErr != nil {
		result.Error = fmt.Sprintf("observe case: %v", observeErr)
		return result
	}
	if closeErr != nil {
		result.Error = fmt.Sprintf("close case: %v", closeErr)
		return result
	}
	result.Status = "complete"
	result.Hash = observation.Hash
	result.EventCount = len(observation.Events)
	if req.IncludeEvents {
		result.Events = json.RawMessage(observation.JSON)
	}
	return result
}

func main() {
	w, err := newWorker()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer w.runtime.Close()

	input := bufio.NewScanner(os.Stdin)
	input.Buffer(make([]byte, 4096), maxRequestBytes)
	output := json.NewEncoder(os.Stdout)
	for input.Scan() {
		var req request
		if err := json.Unmarshal(input.Bytes(), &req); err != nil {
			if encodeErr := output.Encode(response{Status: "error", Error: fmt.Sprintf("decode request: %v", err)}); encodeErr != nil {
				fmt.Fprintln(os.Stderr, encodeErr)
				os.Exit(1)
			}
			continue
		}
		if err := output.Encode(w.run(req)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := input.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
