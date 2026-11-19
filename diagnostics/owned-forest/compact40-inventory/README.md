# Compact ownership source inventory

Baseline dd2afea5. The TSV records each of 40 production and 16 historical test ownership-field occurrences, including declarations, reads, writes and full-node initializers. Counts are historical, not later gated test counts. Source c8f17638 removes common direct ownership selectors; normal build keeps independent booleans and the full 32-bit cold index.

| Source seam | Copy and lifetime contract |
|---|---|
| stack.go storage/elem; operand_links_{list,compact}.go | Only compact uses cold-word bits 31 live and 30 pending. Kind, slot, captured bounds and logical-root metadata remain independent. No added node sidecar. |
| stack.go pushValue; tagged setFreshStorage | Compact strips both donor bits before new-node attachment sets live. |
| references.go replaceStorage; tagged setStoragePreservingOwnership | Strip donor bits; keep original live/pending bits of exact destination. Existing GC/logical-root preservation remains independent. Custom transfer sees masked index. |
| regalloc.go occupy; fp.go occupyF; stack.go setElemKind | Field rewrites never overwrite cold word; pending survives deferred-to-value transition until emit.go checks exact result owner and clears it. |
| stack.go elemCold/setElemCold/clearElemCold; control.go checked ELSE republish | Mask implementation bits for index. Compact rejects custom options before hinting and fails closed at setElemCold. Republish writes fresh whole-node slot image, then explicitly sets live. |
| owned_forest_fixture.go append/replace/erase/retire/peel/rotate, cursor and IF recipe | Live children can be nonroots. Retired and recipe-detached nodes may retain semantic payload/children but are not attached. Pending changes traversal eligibility independent of root metadata. |
| stack.go/operand_arena.go chunk and sentinel lifecycle | Whole-elem zeroing clears compact bits; no arena age, generation, or pointer-stability policy changed. |
| control.go, driver.go, inline, and GC storage snapshots | Existing read-only semantic fields; audited fresh or replace writer controls republish. Raw whole-storage equality is not used as an attachment or identity probe. |

This records source contract only. Actual layout and cross-binary behavior require the gate receipt.
