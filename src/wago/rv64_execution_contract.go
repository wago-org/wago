package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync/atomic"
)

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
	metadata, err := compiledMetadataBytes(c, false)
	if err != nil {
		return err
	}
	if !rv64SourceFrameOwnership(c) {
		return fmt.Errorf("compiled RV64 execution frame ownership is invalid")
	}
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	c.setRV64ExecutionContract(&rv64ExecutionContract{digest: rv64ExecutionDigestProof(c.code, metadata, c.syncHostSlots, rv64StorageFreeProof(c), c.rv64ExecutionTableCapacities()...)})
	return nil
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

func rv64SourceContractValid(c *Compiled) bool {
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
	metadata, err := compiledMetadataBytes(c, false)
	if err != nil || rv64ExecutionDigestProof(c.code, metadata, c.syncHostSlots, rv64StorageFreeProof(c), c.rv64ExecutionTableCapacities()...) != contract.digest || !rv64SourceFrameOwnership(c) {
		return false
	}
	if memo.executionView() == c {
		contract.verified.Store(c)
	}
	return true
}
