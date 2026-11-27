// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_generator && !wago_standalone

package standalone

import (
	"fmt"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/wasmcall"
	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/internal/standalonegen"
)

// GenerateModule compiles source and emits its native-code/metadata Go factory
// for the standalone linker. No Wasm or native-module decoder is needed at run time.
func GenerateModule(source []byte, plugins wago.PluginSet, options Options) (generated map[string][]byte, err error) {
	runtime, err := loadRuntime(plugins, options, nil)
	if err != nil {
		return nil, err
	}
	defer finishRuntime(runtime, &err)
	module, err := runtime.Compile(source)
	if err != nil {
		return nil, err
	}
	defer finishModule(module, &err)
	compiled := module.Compiled()
	main, err := standalonegen.Generate(compiled, "StandaloneCompiled")
	if err != nil {
		return nil, err
	}
	if len(plugins.Providers) == 0 && len(plugins.Selections) == 0 {
		profile, referenceStore, collector := compiled.StandaloneRuntimeProfile()
		invoke, params, results, invocationErr := standaloneInvocationProfile(compiled, options.Invoke)
		resultMask := buildfeatures.StandaloneResultTypes
		if invocationErr == nil {
			resultMask = 0
			for _, result := range results {
				resultMask |= 1 << result
			}
		}
		profile = fmt.Appendf(profile, "\nconst StandaloneHasSignature = %t\nconst StandaloneInvokeExport = %q\nconst StandaloneInvokeFunctionIndex = %d\nconst StandaloneParameterTypes = %q\nconst StandaloneReturnTypes = %q\nconst StandaloneResultTypes = %#x\n",
			invocationErr == nil, invoke, compiled.Exports[invoke], encodeScalarTypes(params), encodeScalarTypes(results), resultMask)
		var unboundFunctionImport string
		var unboundTagImport string
		imports := module.Imports()
		for _, spec := range imports {
			switch spec.Kind {
			case wago.ImportFunc:
				if unboundFunctionImport == "" {
					unboundFunctionImport = spec.Key()
				}
			case wago.ImportTag:
				if unboundTagImport == "" {
					unboundTagImport = spec.Key()
				}
			}
		}
		profile = fmt.Appendf(profile, "const StandaloneUnboundFunctionImport = %q\nconst StandaloneUnboundTagImport = %q\n",
			unboundFunctionImport, unboundTagImport)
		generated := map[string][]byte{
			"standalone_module.go":  main,
			"standalone_profile.go": profile,
		}
		if registers := compiled.StandaloneNativeAssemblyProfile(); len(registers) != 0 {
			generated["standalone_registers.h"] = registers
		}
		if !referenceStore {
			generated["standalone_no_reference_store"] = nil
		}
		if !collector {
			generated["standalone_no_collector"] = nil
		}
		switch {
		case invocationErr != nil:
			generated["standalone_diagnostic.go"] = standaloneDiagnosticSource(invocationErr.Error(), false, nil)
		case unboundFunctionImport != "":
			err := unboundFunctionImportError(unboundFunctionImport)
			generated["standalone_diagnostic.go"] = standaloneDiagnosticSource(err.Error(), invoke != "_start", params)
		default:
			generated["standalone_launcher.go"] = standaloneImageLauncher(options, runtime.Config().CoreFeatures())
			if len(imports) == 0 {
				generated["standalone_one_shot"] = nil
			}
		}
		return generated, nil
	}
	generated = map[string][]byte{"standalone_module.go": main}
	if standalonegen.Dispatcher == nil {
		return nil, fmt.Errorf("standalone generation requires the build-helper profile")
	}
	dispatcher, err := standalonegen.Dispatcher()
	if err != nil {
		return nil, err
	}
	defer dispatcher.(interface{ Close() error }).Close()
	helper, err := standalonegen.Generate(dispatcher, "standaloneManagedDispatcher")
	if err != nil {
		return nil, err
	}
	helper = append(helper, []byte("\nfunc init() { standaloneDispatcherFactory = standaloneManagedDispatcher }\n")...)
	generated["standalone_dispatcher.go"] = helper
	return generated, nil
}

func standaloneImageLauncher(options Options, features wago.CoreFeatures) []byte {
	var featureSelection string
	if options.Core != 0 || options.FeaturesSet {
		featureSelection = fmt.Sprintf(".WithCoreFeatures(%d)", features)
	}
	return fmt.Appendf(nil, `package main

import (
	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/standalone"
)

func main() {
	config := wago.NewRuntimeConfig().WithDeferBoundsChecks(%t)%s
	os.Exit(standalone.RunConfiguredImage(wago.StandaloneCompiled(), config, %q, os.Args))
}
`, options.DeferBoundsChecks, featureSelection, options.Invoke)
}

func standaloneInvocationProfile(compiled *wago.Compiled, requested string) (string, []wago.ValType, []wago.ValType, error) {
	invoke, err := wasmcall.ResolveExport(compiled, requested)
	if err != nil {
		return "", nil, nil, err
	}
	params, results, err := compiled.Signature(invoke)
	if err == nil {
		err = wasmcall.ValidateSignature(params, results)
	}
	if err != nil {
		return "", nil, nil, err
	}
	if invoke == "_start" {
		return invoke, nil, nil, nil
	}
	return invoke, params, results, nil
}

func encodeScalarTypes(types []wago.ValType) string {
	codes := make([]byte, len(types))
	for i, typ := range types {
		codes[i] = byte(typ)
	}
	return string(codes)
}

func standaloneDiagnosticSource(diagnostic string, checkArguments bool, params []wago.ValType) []byte {
	source := []byte(`package main

import (
	"os"
	"path/filepath"
`)
	switch {
	case len(params) != 0:
		source = append(source, "\t\"github.com/wago-org/wago/cli/standalone/arguments\"\n"...)
	case checkArguments:
		source = append(source, "\t\"strconv\"\n"...)
	}
	source = fmt.Appendf(source, `)

func main() {
	name := "program"
	if len(os.Args) != 0 {
		name = filepath.Base(os.Args[0])
	}
	message := %q
`, diagnostic)
	switch {
	case len(params) != 0:
		source = append(source, `	callArgs := os.Args
	if len(callArgs) != 0 {
		callArgs = callArgs[1:]
	}
	params := []arguments.Type{`...)
		for _, param := range params {
			source = fmt.Appendf(source, "%d,", param)
		}
		source = append(source, `}
	if err := arguments.Decode(callArgs, params, nil); err != nil {
		message = err.Error()
	}
`...)
	case checkArguments:
		source = append(source, `	if len(os.Args) > 1 {
		message = "expected 0 arg(s), got " + strconv.Itoa(len(os.Args)-1)
	}
`...)
	}
	return append(source, `	_, _ = os.Stderr.WriteString(name + ": " + message + "\n")
	os.Exit(1)
}
`...)
}
