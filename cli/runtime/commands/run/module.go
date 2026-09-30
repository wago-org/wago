package run

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/internal/wasmcall"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
	"github.com/wago-org/wago/cli/runtime/internal/modulefile"
)

func mustLoadModule(file string, config *wago.RuntimeConfig, runtime *wago.Runtime, cache artifactcache.Cache, allowNativeArtifact bool) *wago.Module {
	module, err := loadModule(file, config, runtime, cache, allowNativeArtifact)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return module
}

func loadModule(file string, config *wago.RuntimeConfig, runtime *wago.Runtime, cache artifactcache.Cache, allowNativeArtifact bool) (*wago.Module, error) {
	source, artifact, artifactFile, size, err := modulefile.ReadSourceOrOpenArtifact(file)
	if err != nil {
		return nil, err
	}
	if artifact != nil {
		defer artifactFile.Close()
		if !allowNativeArtifact {
			return nil, fmt.Errorf("refusing native-code artifact %q; pass --allow-native-artifact only for a trusted .wago file", file)
		}
		compiled, err := loadCompiledArtifactReader(artifact, size)
		if err != nil {
			return nil, err
		}
		module, err := runtime.AdoptModule(compiled)
		if err != nil {
			_ = compiled.Close()
			return nil, err
		}
		return module, nil
	}
	module, err := cache.LoadOrCompile(source, config, runtime)
	if err != nil {
		return nil, err
	}
	return module, nil
}

func loadCompiledArtifact(source []byte) (*wago.Compiled, error) {
	return loadCompiledArtifactReader(bytes.NewReader(source), int64(len(source)))
}

func loadCompiledArtifactReader(source io.Reader, size int64) (*wago.Compiled, error) {
	compiled := new(wago.Compiled)
	read, err := compiled.ReadFromWithLimits(source, wago.DefaultArtifactLimits())
	if err != nil {
		_ = compiled.Close()
		return nil, err
	}
	if size >= 0 && read != size {
		_ = compiled.Close()
		if read < size {
			return nil, fmt.Errorf("trailing %d byte(s) after compiled sections", size-read)
		}
		return nil, fmt.Errorf("compiled artifact changed size while being read")
	}
	var trailing [1]byte
	n, trailingErr := source.Read(trailing[:])
	if n != 0 {
		_ = compiled.Close()
		return nil, fmt.Errorf("trailing data after compiled sections")
	}
	if trailingErr != nil && !errors.Is(trailingErr, io.EOF) {
		_ = compiled.Close()
		return nil, trailingErr
	}
	return compiled, nil
}

func mustResolveExport(compiled *wago.Compiled, requested string) string {
	export, err := wasmcall.ResolveExport(compiled, requested)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return export
}
