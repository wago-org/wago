// Package artifactcache owns automatic reuse of serialized Wago modules.
package artifactcache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/atomicfile"
)

// Cache is a best-effort store for regenerable .wago artifacts. The runtime
// build is represented in every key so plugin-built runtimes and compiler
// upgrades cannot share native code accidentally. Identity overrides the
// embedded Go build identity for tests and custom embedders.
type Cache struct {
	Dir      string
	Identity []byte
	// ReportError observes best-effort publication failures. When nil, failures
	// are reported on stderr so persistent cache misses are diagnosable.
	ReportError func(error)
}

const cacheKeyFormat = 1

var defaultIdentity = sync.OnceValues(func() ([sha256.Size]byte, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return [sha256.Size]byte{}, false
	}
	return buildIdentity(info)
})

// LoadOrCompile loads a matching artifact or compiles source and saves the
// result. Cache read/write failures never prevent execution; compilation and
// artifact validation errors retain their normal behavior.
func (cache Cache) LoadOrCompile(source []byte, config *wago.RuntimeConfig, rt *wago.Runtime) (*wago.Module, error) {
	path, cacheable := cache.path(source, config)
	if cacheable {
		if blob, err := os.ReadFile(path); err == nil {
			compiled := &wago.Compiled{}
			if compiled.UnmarshalBinary(blob) == nil {
				if module, err := rt.Module(compiled); err == nil {
					return module, nil
				}
			}
		}
	}

	module, err := rt.Compile(source)
	if err != nil {
		return nil, err
	}
	if !cacheable {
		return module, nil
	}
	artifact, err := module.Compiled().MarshalBinary()
	if err != nil {
		// Some valid compilation modes intentionally cannot be serialized.
		return module, nil
	}
	if err := publishArtifact(path, artifact); err != nil {
		if cache.ReportError != nil {
			cache.ReportError(err)
		} else {
			fmt.Fprintf(os.Stderr, "wago: artifact cache publication failed: %v\n", err)
		}
	}
	return module, nil
}

func (cache Cache) path(source []byte, config *wago.RuntimeConfig) (string, bool) {
	if cache.Dir == "" || config == nil {
		return "", false
	}
	identity, ok := cache.runtimeIdentity()
	if !ok {
		return "", false
	}
	var storage [256]byte
	encoded := storage[:0]
	encoded = append(encoded, "wago-artifact-cache"...)
	encoded = binary.LittleEndian.AppendUint32(encoded, cacheKeyFormat)
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(len(runtime.GOOS)))
	encoded = append(encoded, runtime.GOOS...)
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(len(runtime.GOARCH)))
	encoded = append(encoded, runtime.GOARCH...)
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(config.CoreFeatures()))
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(config.BoundsChecks()))
	encoded = append(encoded, 0)
	if config.DeferBoundsChecks() {
		encoded[len(encoded)-1] = 1
	}
	encoded = binary.LittleEndian.AppendUint32(encoded, config.MemoryLimitPages())
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(config.FunctionWorkers()))

	knobs := config.OptimizationInfos()
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(knobs)))
	for base := 0; base < len(knobs); base += 8 {
		var selected byte
		for bit := 0; bit < 8 && base+bit < len(knobs); bit++ {
			if knobs[base+bit].On {
				selected |= 1 << bit
			}
		}
		encoded = append(encoded, selected)
	}

	h := sha256.New()
	h.Write(identity[:])
	h.Write(encoded)
	h.Write(source)
	var key [sha256.Size]byte
	h.Sum(key[:0])
	hexKey := hex.EncodeToString(key[:])
	return filepath.Join(cache.Dir, hexKey[:2], hexKey[2:]+".wago"), true
}

func (cache Cache) runtimeIdentity() ([sha256.Size]byte, bool) {
	if len(cache.Identity) != 0 {
		return sha256.Sum256(cache.Identity), true
	}
	return defaultIdentity()
}

func buildIdentity(info *debug.BuildInfo) ([sha256.Size]byte, bool) {
	if info == nil {
		return [sha256.Size]byte{}, false
	}
	cleanRevision := false
	cleanTree := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			cleanRevision = setting.Value != ""
		case "vcs.modified":
			if setting.Value != "false" {
				return [sha256.Size]byte{}, false
			}
			cleanTree = true
		}
	}
	versionedModule := immutableModule(&info.Main, 0)
	if !(cleanRevision && cleanTree) && !versionedModule {
		return [sha256.Size]byte{}, false
	}
	for _, dependency := range info.Deps {
		if !immutableModule(dependency, 0) {
			return [sha256.Size]byte{}, false
		}
	}
	h := sha256.New()
	h.Write([]byte("wago-build-identity"))
	writeString(h, info.GoVersion)
	writeString(h, info.Path)
	writeModule(h, info.Main)
	writeUint32(h, uint32(len(info.Deps)))
	for _, dependency := range info.Deps {
		writeModule(h, *dependency)
	}
	writeUint32(h, uint32(len(info.Settings)))
	for _, setting := range info.Settings {
		writeString(h, setting.Key)
		writeString(h, setting.Value)
	}
	var identity [sha256.Size]byte
	copy(identity[:], h.Sum(nil))
	return identity, true
}

func immutableModule(module *debug.Module, depth int) bool {
	if module == nil || depth > 8 {
		return false
	}
	if module.Replace != nil {
		return immutableModule(module.Replace, depth+1)
	}
	return module.Path != "" && module.Version != "" && module.Version != "(devel)" && module.Sum != ""
}

func writeModule(h interface{ Write([]byte) (int, error) }, module debug.Module) {
	writeString(h, module.Path)
	writeString(h, module.Version)
	writeString(h, module.Sum)
	if module.Replace == nil {
		h.Write([]byte{0})
		return
	}
	h.Write([]byte{1})
	writeModule(h, *module.Replace)
}

func writeString(h interface{ Write([]byte) (int, error) }, value string) {
	writeUint64(h, uint64(len(value)))
	h.Write([]byte(value))
}

func writeUint32(h interface{ Write([]byte) (int, error) }, value uint32) {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	h.Write(encoded[:])
}

func writeUint64(h interface{ Write([]byte) (int, error) }, value uint64) {
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], value)
	h.Write(encoded[:])
}

var publishArtifact = writeAtomic

func writeAtomic(path string, artifact []byte) error {
	return atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0o644}, func(writer io.Writer) error {
		_, err := writer.Write(artifact)
		return err
	})
}
