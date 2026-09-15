package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

var functionCacheSnapshotMagic = [8]byte{'W', 'A', 'G', 'O', 'F', 'N', 'C', '1'}

const functionCacheSnapshotVersion uint32 = 1

type functionCacheSnapshotEntry struct {
	key      [32]byte
	artifact FunctionArtifact
}

// SnapshotTo writes a deterministic, versioned snapshot of the cache's current
// immutable entries. Operational counters and LRU ages are intentionally not
// persisted. The returned byte count includes the header.
func (c *FunctionArtifactCache) SnapshotTo(w io.Writer) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("compiler function cache: nil cache")
	}
	if w == nil {
		return 0, fmt.Errorf("compiler function cache: nil snapshot writer")
	}
	c.mu.Lock()
	entries := make([]functionCacheSnapshotEntry, 0, len(c.entries))
	for key, entry := range c.entries {
		entries = append(entries, functionCacheSnapshotEntry{key: key, artifact: cloneFunctionArtifact(entry.artifact)})
	}
	c.mu.Unlock()
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key[:], entries[j].key[:]) < 0 })
	if uint64(len(entries)) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("compiler function cache: too many snapshot entries")
	}

	var header [16]byte
	copy(header[:8], functionCacheSnapshotMagic[:])
	binary.LittleEndian.PutUint32(header[8:12], functionCacheSnapshotVersion)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(entries)))
	written, err := writeFunctionCacheSnapshotBytes(w, header[:])
	if err != nil {
		return written, err
	}
	for _, entry := range entries {
		encoded, err := MarshalFunctionArtifact(entry.artifact)
		if err != nil {
			return written, fmt.Errorf("compiler function cache: snapshot artifact: %w", err)
		}
		if uint64(len(encoded)) > uint64(^uint32(0)) {
			return written, fmt.Errorf("compiler function cache: snapshot artifact is too large")
		}
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(encoded)))
		n, err := writeFunctionCacheSnapshotBytes(w, size[:])
		written += n
		if err != nil {
			return written, err
		}
		n, err = writeFunctionCacheSnapshotBytes(w, encoded)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// RestoreFrom atomically replaces the cache entries from one deterministic
// snapshot. Every artifact is decoded and charged against MaxBytes before the
// live cache is changed. Operational hit/miss/eviction counters are retained.
func (c *FunctionArtifactCache) RestoreFrom(r io.Reader) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("compiler function cache: nil cache")
	}
	if r == nil {
		return 0, fmt.Errorf("compiler function cache: nil snapshot reader")
	}
	var header [16]byte
	read, err := readFunctionCacheSnapshotBytes(r, header[:])
	if err != nil {
		return read, err
	}
	if !bytes.Equal(header[:8], functionCacheSnapshotMagic[:]) {
		return read, fmt.Errorf("compiler function cache: invalid snapshot magic")
	}
	if version := binary.LittleEndian.Uint32(header[8:12]); version != functionCacheSnapshotVersion {
		return read, fmt.Errorf("compiler function cache: snapshot version %d unsupported", version)
	}
	count := binary.LittleEndian.Uint32(header[12:16])
	if uint64(count) > c.maxBytes/(functionCacheEntryCharge+1) {
		return read, fmt.Errorf("compiler function cache: snapshot entry count exceeds cache budget")
	}
	entries := make(map[[32]byte]cachedFunctionArtifact, count)
	charged := uint64(0)
	clock := uint64(0)
	for index := uint32(0); index < count; index++ {
		var size [4]byte
		n, err := readFunctionCacheSnapshotBytes(r, size[:])
		read += n
		if err != nil {
			return read, err
		}
		length := uint64(binary.LittleEndian.Uint32(size[:]))
		charge := length + functionCacheEntryCharge
		if length == 0 || charge < length || charge > c.maxBytes || charged > c.maxBytes-charge {
			return read, fmt.Errorf("compiler function cache: snapshot exceeds cache budget")
		}
		encoded := make([]byte, int(length))
		n, err = readFunctionCacheSnapshotBytes(r, encoded)
		read += n
		if err != nil {
			return read, err
		}
		artifact, err := UnmarshalFunctionArtifact(encoded)
		if err != nil {
			return read, fmt.Errorf("compiler function cache: snapshot artifact %d: %w", index, err)
		}
		key := artifact.IdentityFingerprint
		if _, exists := entries[key]; exists {
			return read, fmt.Errorf("compiler function cache: duplicate snapshot artifact %x", key)
		}
		clock++
		entries[key] = cachedFunctionArtifact{artifact: artifact, charge: charge, used: clock}
		charged += charge
	}
	var trailing [1]byte
	n, trailingErr := r.Read(trailing[:])
	read += int64(n)
	if n != 0 || trailingErr == nil {
		return read, fmt.Errorf("compiler function cache: trailing snapshot data")
	}
	if trailingErr != io.EOF {
		return read, fmt.Errorf("compiler function cache: read snapshot trailer: %w", trailingErr)
	}
	c.mu.Lock()
	c.entries = entries
	c.charged = charged
	c.clock = clock
	c.mu.Unlock()
	return read, nil
}

func writeFunctionCacheSnapshotBytes(w io.Writer, data []byte) (int64, error) {
	written := int64(0)
	for len(data) != 0 {
		n, err := w.Write(data)
		written += int64(n)
		data = data[n:]
		if err != nil {
			return written, fmt.Errorf("compiler function cache: write snapshot: %w", err)
		}
		if n == 0 {
			return written, fmt.Errorf("compiler function cache: write snapshot: %w", io.ErrNoProgress)
		}
	}
	return written, nil
}

func readFunctionCacheSnapshotBytes(r io.Reader, data []byte) (int64, error) {
	n, err := io.ReadFull(r, data)
	if err != nil {
		return int64(n), fmt.Errorf("compiler function cache: read snapshot: %w", err)
	}
	return int64(n), nil
}
