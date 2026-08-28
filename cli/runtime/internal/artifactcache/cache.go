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
	"time"

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
	// MaxBytes caps regular .wago files below Dir. Zero uses DefaultMaxBytes.
	MaxBytes int64
	// ReportError observes best-effort publication failures. When nil, failures
	// are reported on stderr so persistent cache misses are diagnosable.
	ReportError func(error)
}

const cacheKeyFormat = 2

// DefaultMaxBytes bounds the automatic CLI artifact cache at 512 MiB.
const DefaultMaxBytes int64 = 512 << 20

// maxPruneEntries bounds both cache file count and prune's in-memory index.
const maxPruneEntries = 4096

// maxCacheDirectoryDepth bounds descriptors retained by recursive batched scans.
// Generated cache entries use one shard level; deeper trees are corruption.
const maxCacheDirectoryDepth = 8

const (
	cachePruneInterval = 5 * time.Minute
	cachePruneMarker   = ".wago-prune"
)

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
func (cache Cache) LoadOrCompile(source []byte, _ *wago.RuntimeConfig, rt *wago.Runtime) (*wago.Module, error) {
	if rt == nil {
		return nil, fmt.Errorf("wago: artifact cache requires a runtime")
	}
	// Runtime.Compile is authoritative. The explicit config parameter is retained
	// for source compatibility, but cannot select code under a different policy.
	config := rt.Config()
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
			if err := cache.pruneIfDue(time.Now()); err != nil {
				cache.report(err)
			}
			return prepared.Adopt(compiled)
		}
	}

	module, err := prepared.Compile()
	if err != nil {
		return nil, err
	}
	if !cacheable {
		if err := cache.pruneIfDue(time.Now()); err != nil {
			cache.report(err)
		}
		return module, nil
	}
	limit := cache.MaxBytes
	if limit == 0 {
		limit = DefaultMaxBytes
	}
	sizes, err := module.Compiled().ArtifactSectionSizes()
	if err != nil {
		cache.report(err)
		return module, nil
	}
	if !artifactFitsCache(sizes, limit) {
		if err := cache.pruneIfDue(time.Now()); err != nil {
			cache.report(err)
		}
		return module, nil
	}
	if err := publishArtifact(path, module.Compiled()); err != nil {
		cache.report(err)
	} else if err := cache.pruneAndMark(); err != nil {
		cache.report(err)
	}
	return module, nil
}

func artifactFitsCache(sizes wago.ArtifactSectionSizes, maxBytes int64) bool {
	limits := wago.DefaultArtifactLimits()
	return sizes.Code <= limits.MaxCodeBytes && sizes.Metadata <= limits.MaxMetadataBytes &&
		(maxBytes < 0 || sizes.Total <= maxBytes)
}

func (cache Cache) pruneIfDue(now time.Time) error {
	if cache.MaxBytes < 0 || cache.Dir == "" {
		return nil
	}
	marker := filepath.Join(cache.Dir, cachePruneMarker)
	info, err := os.Lstat(marker)
	if err == nil {
		if info.Mode().IsRegular() {
			age := now.Sub(info.ModTime())
			if age >= 0 && age < cachePruneInterval {
				return nil
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err = os.Stat(cache.Dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("cache root %s is not a directory", cache.Dir)
	}
	return cache.pruneAndMark()
}

func (cache Cache) pruneAndMark() error {
	if err := cache.prune(); err != nil {
		return err
	}
	if cache.MaxBytes < 0 || cache.Dir == "" {
		return nil
	}
	marker := filepath.Join(cache.Dir, cachePruneMarker)
	if info, err := os.Lstat(marker); err == nil && !info.Mode().IsRegular() {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return atomicfile.ReplaceFile(marker, atomicfile.Options{Mode: 0o600}, func(io.Writer) error { return nil })
}

func (cache Cache) report(err error) {
	if cache.ReportError != nil {
		cache.ReportError(err)
	} else {
		fmt.Fprintf(os.Stderr, "wago: artifact cache maintenance failed: %v\n", err)
	}
}

type cacheEntry struct {
	path         string
	relativePath string
	size         int64
	modTime      time.Time
}

type cacheEntryHeap []cacheEntry

func pushCacheEntry(entries *cacheEntryHeap, entry cacheEntry) {
	h := append(*entries, entry)
	for child := len(h) - 1; child > 0; {
		parent := (child - 1) / 2
		if !cacheEntryOlder(h[child], h[parent]) {
			break
		}
		h[parent], h[child] = h[child], h[parent]
		child = parent
	}
	*entries = h
}

func popOldestCacheEntry(entries *cacheEntryHeap) cacheEntry {
	h := *entries
	oldest := h[0]
	last := len(h) - 1
	h[0] = h[last]
	h[last] = cacheEntry{}
	h = h[:last]
	for parent := 0; ; {
		left := parent*2 + 1
		if left >= len(h) {
			break
		}
		child := left
		right := left + 1
		if right < len(h) && cacheEntryOlder(h[right], h[left]) {
			child = right
		}
		if !cacheEntryOlder(h[child], h[parent]) {
			break
		}
		h[parent], h[child] = h[child], h[parent]
		parent = child
	}
	*entries = h
	return oldest
}

func (cache Cache) prune() error {
	limit := cache.MaxBytes
	if limit == 0 {
		limit = DefaultMaxBytes
	}
	if limit < 0 || cache.Dir == "" {
		return nil
	}
	root, err := openCacheRoot(cache.Dir)
	if err != nil {
		return err
	}
	defer root.close()
	var total int64
	entries := make(cacheEntryHeap, 0, maxPruneEntries)
	remove := func(entry cacheEntry) error {
		if err := root.remove(entry.relativePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	visit := func(relativePath, path string, info os.FileInfo) error {
		entry := cacheEntry{path: path, relativePath: relativePath, size: info.Size(), modTime: info.ModTime()}
		if len(entries) < maxPruneEntries {
			pushCacheEntry(&entries, entry)
			total += entry.size
			return nil
		}
		oldest := entries[0]
		if !cacheEntryNewer(entry, oldest) {
			return remove(entry)
		}
		if err := remove(oldest); err != nil {
			return err
		}
		popOldestCacheEntry(&entries)
		total -= oldest.size
		pushCacheEntry(&entries, entry)
		total += entry.size
		return nil
	}
	err = root.scan(visit)
	if err != nil || total <= limit {
		return err
	}
	for total > limit && len(entries) != 0 {
		entry := popOldestCacheEntry(&entries)
		if err := remove(entry); err != nil {
			return err
		}
		total -= entry.size
	}
	return nil
}

func cacheEntryNewer(left, right cacheEntry) bool {
	if left.modTime.Equal(right.modTime) {
		return left.path > right.path
	}
	return left.modTime.After(right.modTime)
}

func cacheEntryOlder(left, right cacheEntry) bool {
	return cacheEntryNewer(right, left)
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
