# Baseline construction allocation ownership

The baseline already includes the earlier snapshot fix. Profile: imports-base.alloc, 10,002 raw bundle constructions including benchmark calibration and the oracle. Counts below divide allocation-profile objects by those calls; small-object packing means they are not the same as testing allocs/op. Reported complete construction remains 374 allocs/op and 32,648 B/op. Bytes are approximate per command from allocation sites, not retained-memory measurements.

| Site | Profile objects / bytes per construction | Kind and owner | Required lifetime | Proposed change |
| --- | --- | --- | --- | --- |
| Imports.HostFunc | 99 / 11,688 B | Final mutable declarations, builders, declaration slice; Wago Imports | Until bundle/registration is released; builder lifetime can extend through public API | Leave unchanged in first patch; co-allocation would need separate tests of builder and clone lifetimes |
| Plugin.bindings | 55 / 6,608 B | Fixed definitions and signatures plus 46 instance-bound inner closures; WASI provider | Definition fields immutable; closures belong to bundle/provider | Fixed private definition table; remove intermediate bound fn records |
| binding.callback | 46 / 5,888 B | Outer adapter captures a 112-byte binding value; provider-owned | Bundle/provider lifetime | One callback captures only handler and owning Plugin; same guarded dispatch |
| Imports.add | 8 / 4,424 B, excluding key child | Import binding map storage; Wago Imports | Sealed bundle lifetime | Leave validation and map ownership unchanged |
| importBindingMapKey | 46 / 2,144 B | Exact module/function key; Wago Imports | Binding map lifetime | Required registration keys remain; snapshot duplicate was already removed |
| ImportFuncBuilder.Params | about 23 / 368 B | Defensive declaration copy; Wago Imports | Declaration lifetime | Keep public copy; provider's source signature can become private immutable data |
| ImportFuncBuilder.Results | about 22.5 / 360 B | Defensive declaration copy; Wago Imports | Declaration lifetime | Keep public copy |
| Plugin.makeFS | 7 / 672 B for this no-mount configuration | fd records/maps and state; WASI provider | Per command/instance | Keep fresh, with existing locking and descriptor ownership |
| core.Imports direct allocation | 1 / 224 B | Plugin/config object; WASI provider | Bundle lifetime | Keep fresh |
| NewImports/resetFS/config and adapter inputs | Smaller remaining allocations | Mixed fresh containers and caller configuration copies | Bundle or call-specific | Retain; no unbounded cache or state sharing |

Escape analysis confirms that bindings' intermediate closures escape, that callbacks capture the full binding value (112 bytes), and that the binding table/signature slices escape. It also shows the per-host-call Plugin copy escaping through the handler call; the optimization must not increase host-call allocations or add a lock/lookup.

The selected candidate changes the provider-owned immutable definition and adapter representation only. It keeps Params/Results defensive copies, fresh Plugin/fs state, stateFor lookup and lock order, instance identity checks, handler checks, and resource shutdown unchanged. One bounded package-private definition table is shared; no mutable table backing is returned through a public API.

## Measured candidate

The final provider patch removes 70 logical allocations per construction: one binding array, 23 signature-literal backing arrays, and 46 inner closures. Profile object counts differ because small allocations are packed. The removed per-command definition work accounts for 6,608 bytes. The remaining 46 outer callbacks shrink from 128-byte allocation classes to 24 bytes each, saving another 4,784 bytes. Total measured reduction is exactly 11,392 B/op, from 32,648 to 21,256 B/op. The private fixed table instead allocates about 5 KiB once per process.

The final profile still attributes 99 objects per construction to HostFunc (records, temporary builders, slice growth), 46 import keys, the 46 owner-specific callbacks, required public signature copies, import-map growth and fresh filesystem/configuration state. These remaining sites were not changed. No unsupported builder or ownership-boundary copy removal is claimed.
