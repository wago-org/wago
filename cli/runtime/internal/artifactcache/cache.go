// Package artifactcache owns automatic reuse of serialized Wago modules.
package artifactcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wago-org/wago"
)

// Cache is a best-effort store for regenerable .wago artifacts. Executable is
// hashed into every key so plugin-built runtimes and compiler upgrades cannot
// share native code accidentally. Identity is an injectable equivalent used by
// tests and embedders that do not have a stable executable path.
type Cache struct {
	Dir        string
	Executable string
	Identity   []byte
}

type signature struct {
	Source             [sha256.Size]byte  `json:"source"`
	Runtime            [sha256.Size]byte  `json:"runtime"`
	GOOS               string             `json:"goos"`
	GOARCH             string             `json:"goarch"`
	Features           uint64             `json:"features"`
	BoundsChecks       string             `json:"boundsChecks"`
	DeferredBounds     bool               `json:"deferredBoundsChecks"`
	MaximumMemoryPages uint32             `json:"maximumMemoryPages"`
	OptimizationKnobs  []optimizationKnob `json:"optimizationKnobs"`
}

type optimizationKnob struct {
	Name string `json:"name"`
	On   bool   `json:"on"`
}

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
	_ = writeAtomic(path, artifact)
	return module, nil
}

func (cache Cache) path(source []byte, config *wago.RuntimeConfig) (string, bool) {
	if cache.Dir == "" || config == nil {
		return "", false
	}
	identity, err := cache.runtimeIdentity()
	if err != nil {
		return "", false
	}
	knobs := wago.OptKnobs()
	sig := signature{
		Source:             sha256.Sum256(source),
		Runtime:            identity,
		GOOS:               runtime.GOOS,
		GOARCH:             runtime.GOARCH,
		Features:           uint64(config.CoreFeatures()),
		BoundsChecks:       config.BoundsChecks().String(),
		DeferredBounds:     config.DeferBoundsChecks(),
		MaximumMemoryPages: config.MemoryLimitPages(),
		OptimizationKnobs:  make([]optimizationKnob, len(knobs)),
	}
	for index, knob := range knobs {
		sig.OptimizationKnobs[index] = optimizationKnob{Name: knob.Name, On: knob.On}
	}
	encoded, err := json.Marshal(sig)
	if err != nil {
		return "", false
	}
	key := sha256.Sum256(encoded)
	hexKey := hex.EncodeToString(key[:])
	return filepath.Join(cache.Dir, hexKey[:2], hexKey[2:]+".wago"), true
}

func (cache Cache) runtimeIdentity() ([sha256.Size]byte, error) {
	if len(cache.Identity) != 0 {
		return sha256.Sum256(cache.Identity), nil
	}
	if cache.Executable == "" {
		return [sha256.Size]byte{}, errors.New("artifact cache: runtime identity unavailable")
	}
	binary, err := os.ReadFile(cache.Executable)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(binary), nil
}

func writeAtomic(path string, artifact []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".wago-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(artifact); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
