package wago

import (
	"fmt"
)

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
