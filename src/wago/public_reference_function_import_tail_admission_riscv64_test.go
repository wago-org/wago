//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"

	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func rv64ReferenceImportTailCopy(c *Compiled) *Compiled {
	v := cloneCompiledMetadata(c.executionView())
	if cache := c.loadCodeCache(); cache != nil {
		// Each mutant owns its staging cache. Do not copy a mutex or share an
		// executable mapping with the unchanged fixture.
		v.codeCache = &compiledCodeCache{
			gcMetadataFlags:        cache.gcMetadataFlags,
			gcTypeSubtypingProduct: cache.gcTypeSubtypingProduct,
			gcStructProduct:        cache.gcStructProduct,
			gcArrayProduct:         cache.gcArrayProduct,
			gcI31Product:           cache.gcI31Product,
			flags:                  cache.flags,
			stagedFeatures:         cache.stagedFeatures,
		}
	}
	indexes := *c.loadCompileIndexes()
	if module := rv64NativeTailModule(c); module != nil {
		indexes.railshotTail.module = &encoder.NativeTailModule{Plans: slices.Clone(module.Plans)}
	}
	v.validateMemo = &validateMemo{compileIndexes: &indexes, importModuleEnds: slices.Clone(c.loadValidateMemo().importModuleEnds)}
	return v
}
