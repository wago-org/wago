//go:build riscv64

package riscv64

import "os"

// branchFoldEnabled controls the lowering-time branch-edge fold implemented by
// condBranchJump. That fold emits native fixed-size RV64 branches directly; it
// does not rewrite finalized instruction words.
var branchFoldEnabled = os.Getenv("WAGO_RISCV64_NOBRFOLD") != "1"

// Post-assembly rewriting stays disabled until there is an RV64 instruction
// decoder and rewrite with branch-target safety tests. Treating RISC-V words as
// another architecture's words can silently corrupt otherwise valid code.

// finalizePeepholes intentionally performs no binary rewriting on RV64. All
// currently enabled peepholes run while lowering, before branch and call
// relocations are finalized.
func (f *fn) finalizePeepholes() {}
