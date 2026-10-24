package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync/atomic"
)

// The contract is a consistency binding within LoadTrustedArtifact's explicit
// authenticated-native-code trust boundary. It cannot authenticate hostile code.
// No compiler receipt, native pointer, or process key is serialized.
var rv64ExecutionContractMarker = [8]byte{'R', 'V', '6', '4', 'E', 'X', 'E', 'C'}

const rv64ExecutionContractBytes = 8 + 1 + 2 + sha256.Size
const maxRV64ExecutionTables = 1024

type rv64ExecutionContract struct {
	digest   [sha256.Size]byte
	verified atomic.Pointer[Compiled] // only the private frozen execution snapshot
	rooted   atomic.Pointer[Compiled] // complete rooted admission for that same snapshot
}

func installRV64SourceExecutionContract(c *Compiled) error {
	if compiledArchitecture != 4 {
		return nil
	}
	if err := railshotValidateTableCapacities(c); err != nil {
		return err
	}
	metadata, _, err := encodeCompiledMetadataBody(c, false, false)
	if err != nil {
		return err
	}
	if !rv64ArtifactFrameOwnership(c) {
		return fmt.Errorf("compiled RV64 execution frame ownership is invalid")
	}
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	c.setRV64ExecutionContract(&rv64ExecutionContract{digest: rv64ExecutionDigestProof(c.code, metadata, c.syncHostSlots, rv64StorageFreeProof(c), c.rv64ExecutionTableCapacities()...)})
	return nil
}

func hasRV64ExecutionContract(c *Compiled) bool {
	memo := c.loadValidateMemo()
	return memo != nil && (c.rv64ExecutionContract() != nil || memo.rv64FreshRootedGC)
}

func rv64ExecutionDigest(code, metadata []byte, slots uint16, capacities ...uint32) [sha256.Size]byte {
	return rv64ExecutionDigestProof(code, metadata, slots, false, capacities...)
}

func rv64ExecutionDigestProof(code, metadata []byte, slots uint16, storageFree bool, capacities ...uint32) [sha256.Size]byte {
	h := sha256.New()
	if storageFree {
		h.Write([]byte("wago RV64 execution contract storage-free bodies v3 architecture4\x00"))
	} else if len(capacities) == 0 {
		h.Write([]byte("wago RV64 execution contract v1 architecture4\x00"))
	} else {
		h.Write([]byte("wago RV64 execution contract v2 architecture4\x00"))
	}
	var lengths [18]byte
	binary.LittleEndian.PutUint64(lengths[:8], uint64(len(code)))
	binary.LittleEndian.PutUint64(lengths[8:16], uint64(len(metadata)))
	binary.LittleEndian.PutUint16(lengths[16:], slots)
	h.Write(lengths[:])
	h.Write(code)
	h.Write(metadata)
	if len(capacities) != 0 {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], uint32(len(capacities)))
		h.Write(word[:])
		for _, capacity := range capacities {
			binary.LittleEndian.PutUint32(word[:], capacity)
			h.Write(word[:])
		}
	}
	var digest [sha256.Size]byte
	h.Sum(digest[:0])
	return digest
}

func rv64ExecutionContractTrailer(data []byte) bool {
	return compiledArchitecture == 4 && len(data) >= rv64ExecutionContractBytes && string(data[:8]) == string(rv64ExecutionContractMarker[:])
}

func rv64ExecutionContractSize(c *Compiled) int {
	size := rv64ExecutionContractBytes
	if rv64StorageFreeProof(c) {
		size++
	}
	if n := len(c.rv64ExecutionTableCapacities()); n != 0 {
		size += 4 + 4*n
	}
	return size
}

func readRV64ExecutionContract(c *Compiled, trailer, metadata []byte) error {
	if !rv64ExecutionContractTrailer(trailer) || trailer[8] < 1 || trailer[8] > 4 {
		return fmt.Errorf("unsupported RV64 native execution contract")
	}
	version := trailer[8]
	head, size := 11, rv64ExecutionContractBytes
	storageFree := version >= 3
	if storageFree {
		if len(trailer) < size+1 || trailer[9] != 1 {
			return fmt.Errorf("invalid RV64 storage-free execution flags")
		}
		head++
		size++
	}
	setRV64StorageFreeProof(c, storageFree)
	slotOffset := head - 2
	c.syncHostSlots = binary.LittleEndian.Uint16(trailer[slotOffset:head])
	digestOffset := head
	if version == 1 || version == 3 {
		if len(trailer) != size {
			return fmt.Errorf("invalid RV64 native execution contract length")
		}
	} else {
		if len(trailer) < size+4 {
			return fmt.Errorf("truncated RV64 execution table capacities")
		}
		count := binary.LittleEndian.Uint32(trailer[head : head+4])
		if count == 0 || count > maxRV64ExecutionTables || int(count) != c.tableCount() || len(trailer) != size+4+4*int(count) {
			return fmt.Errorf("invalid RV64 execution table capacity count or length")
		}
		var small [8]uint32
		capacities := small[:min(int(count), len(small))]
		if int(count) > len(small) {
			capacities = make([]uint32, count)
		}
		for i := range capacities {
			capacities[i] = binary.LittleEndian.Uint32(trailer[head+4+4*i:])
		}
		c.setRV64ExecutionTableCapacities(capacities)
		digestOffset = head + 4 + 4*int(count)
	}
	digest := rv64ExecutionDigestProof(c.code, metadata, c.syncHostSlots, storageFree, c.rv64ExecutionTableCapacities()...)
	if string(trailer[digestOffset:]) != string(digest[:]) {
		return fmt.Errorf("RV64 native execution contract code/metadata binding mismatch")
	}
	c.setRV64ExecutionContract(&rv64ExecutionContract{digest: digest})
	if !rv64ArtifactContractValid(c) {
		return fmt.Errorf("RV64 native execution contract has invalid frame or metadata ownership")
	}
	return nil
}

func rv64ArtifactContractValid(c *Compiled) bool {
	if c == nil || compiledArchitecture != 4 {
		return false
	}
	memo := c.loadValidateMemo()
	if memo == nil || c.rv64ExecutionContract() == nil {
		return false
	}
	contract := c.rv64ExecutionContract()
	if memo.executionView() == c && contract.verified.Load() == c && (len(c.Funcs) == 0 || len(c.code) != 0) {
		return true
	}
	if railshotValidateTableCapacities(c) != nil {
		return false
	}
	// Encoding exact import names indexes each imported directory entry. A
	// mutable or hand-built view must prove those bounds before digest encoding.
	if c.validateImportModuleEnds() != nil || c.validateMemoryMetadata(c.requiredFeatures) != nil {
		return false
	}
	metadata, _, err := encodeCompiledMetadataBody(c, false, false)
	if err != nil || rv64ExecutionDigestProof(c.code, metadata, c.syncHostSlots, rv64StorageFreeProof(c), c.rv64ExecutionTableCapacities()...) != contract.digest || !rv64ArtifactFrameOwnership(c) {
		return false
	}
	if memo.executionView() == c {
		contract.verified.Store(c)
	}
	return true
}

func rv64PopulateArtifactReceipt(memo *validateMemo, c *Compiled) bool {
	var ok bool
	if memo.rv64FreshRootedGCTypeDigest, ok = rv64RootedTypeDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCImportDigest, ok = rv64RootedImportDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCDataDigest, ok = rv64RootedActiveDataDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCExportDigest, ok = rv64RootedMemoryExportDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCGlobalDigest, ok = rv64RootedGlobalDigest(c); !ok {
		return false
	}
	if memo.rv64FreshRootedGCTableDigest, ok = rv64RootedTableDigest(c); !ok {
		return false
	}
	memo.rv64FreshRootedGC = true
	memo.rv64FreshRootedGCDigest = sha256.Sum256(c.code)
	memo.rv64FreshRootedGCSyncHostSlots = c.syncHostSlots
	memo.rv64FreshRootedGCReserveMax = c.MemMaxPages
	memo.rv64FreshRootedGCHasStart = c.HasStart
	if c.HasStart {
		memo.rv64FreshRootedGCStartImport = c.StartIsImport
		if c.StartIsImport {
			memo.rv64FreshRootedGCStartImportIndex = uint32(c.StartImportIdx)
		} else {
			memo.rv64FreshRootedGCStartLocal = uint32(c.StartLocalFunc)
		}
	}
	if directory := c.memoryDir; directory != nil {
		memo.rv64FreshRootedGCMemories = uint8(len(directory.defs))
		memo.rv64FreshRootedGCMemoryStaged = directory.staged
		if len(directory.defs) != 0 {
			memo.rv64FreshRootedGCMemMin, memo.rv64FreshRootedGCMemMax = directory.defs[0].Min, directory.defs[0].Max
		}
	}
	return true
}
