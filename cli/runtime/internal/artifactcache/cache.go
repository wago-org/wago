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

const cacheKeyFormat = 2

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
	if rt == nil {
		return nil, fmt.Errorf("wago: artifact cache requires a runtime")
	}
	// Runtime.Compile is authoritative. The explicit config parameter is retained
	// for source compatibility, but cannot select code under a different policy.
	config = rt.Config()
	if config == nil {
		return nil, fmt.Errorf("wago: artifact cache runtime has no configuration")
	}
	// Validate before lookup, bypass, or plugin preparation: a warm entry must not
	// admit a configuration that a cold Runtime.Compile rejects.
	if err := config.Validate(); err != nil {
		return nil, err
	}
	prepared, err := rt.PrepareCompile(source)
	if err != nil {
		return nil, err
	}
	defer prepared.Close()

	cacheableGeneration := prepared.Cacheable()
	if config.GCCodeTelemetry() || config.BoundsChecks() == wago.BoundsChecksSignalsBased {
		// Telemetry is compile-only and absent from serialized artifacts. Signals-
		// based native code is also deliberately nonserializable.
		cacheableGeneration = false
	}
	path, cacheable := cache.path(prepared.Source(), config)
	cacheable = cacheable && cacheableGeneration
	if cacheable {
		if compiled, hit := loadArtifact(path); hit {
			return prepared.Adopt(compiled)
		}
	}

	module, err := prepared.Compile()
	if err != nil {
		return nil, err
	}
	if !cacheable {
		return module, nil
	}
	if err := publishArtifact(path, module.Compiled()); err != nil {
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

func loadArtifact(path string) (*wago.Compiled, bool) {
	linked, err := os.Lstat(path)
	if err != nil || linked.Mode()&os.ModeSymlink != 0 || !linked.Mode().IsRegular() {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(linked, opened) {
		return nil, false
	}
	return loadOpenedArtifact(path, file, opened)
}

func loadOpenedArtifact(path string, file *os.File, opened os.FileInfo) (*wago.Compiled, bool) {
	limits := wago.DefaultArtifactLimits()
	maximum := limits.MaxCodeBytes + limits.MaxMetadataBytes + 64
	if opened.Size() < 0 || opened.Size() > maximum {
		return nil, false
	}
	compiled := &wago.Compiled{}
	read, err := compiled.ReadFromWithLimits(file, limits)
	if err != nil {
		_ = compiled.Close()
		return nil, false
	}
	var trailing [1]byte
	trailingBytes, trailingErr := file.Read(trailing[:])
	finalOpened, statErr := file.Stat()
	finalLinked, linkErr := os.Lstat(path)
	if read != opened.Size() || trailingBytes != 0 || trailingErr != io.EOF ||
		statErr != nil || finalOpened.Size() != read || !finalOpened.Mode().IsRegular() ||
		linkErr != nil || finalLinked.Mode()&os.ModeSymlink != 0 || !finalLinked.Mode().IsRegular() ||
		!os.SameFile(finalLinked, finalOpened) {
		_ = compiled.Close()
		return nil, false
	}
	return compiled, true
}

func writeAtomic(path string, compiled *wago.Compiled) error {
	return atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0o644}, func(writer io.Writer) error {
		_, err := compiled.WriteTo(writer)
		return err
	})
}
