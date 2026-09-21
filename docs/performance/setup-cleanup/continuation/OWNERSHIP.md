# Resource ownership before optimization

Pinned WASI base: 6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e. Source: internal/core/core.go and fs.go. No lifecycle production change has been made.

| Resource | Creator and owner | Borrowed/owned | Required lifetime and cleanup | Partial setup |
| --- | --- | --- | --- | --- |
| Args, Env, Mounts configuration slices | Caller supplies them; raw Imports clones them | Provider-owned copies; underlying strings immutable | Bundle/provider lifetime | GC after failed construction; no caller slice mutation |
| Stdin, Stdout, Stderr, Rand, Clocks, Context | Caller | Borrowed references | Provider does not close streams; caller controls external lifetime | Do not close on failure |
| Stdio descriptor records | makeFS | State owns records, borrows readers/writers | closeFS clears records without calling stream Close | closeFS clears records |
| Preopen os.File and directory root | makeFS/openPreopen | Provider filesystem state owns them | Provider closeInstance or closeAll; explicit guest fd_close also supported | Strict makeFS closes prior successful opens on error; raw non-strict path retains successful mounts and skips failed mounts |
| Guest-opened os.File and directory iterator | path_open/readdir | Owning instance filesystem state | fd_close, instance-close observer, or provider Stop | Newly failed opens close their temporary file; retained entries stay owned by state |
| Per-instance fd maps and locks | stateFor/makeFS | Private to instance identity in Provider mode | Observer deletes state and closes its files; Stop joins synchronized state access through existing locks | If instance setup fails before it claims state, provider retains unclaimed initial state until Stop |
| Provider initial, unclaimed filesystem state | Plugin.Start | Provider/runtime | Claimed by first host caller or closed by provider Stop | Runtime cleanup required after failure |
| Raw p1.Imports bundle | core.Imports | One instance's raw bundle | Contract excludes provider lifecycle cleanup; no exported p1 cleanup handle | Raw construction cannot return mount errors; Instance.Close does not add provider cleanup |

Provider Register installs InstanceCloseObserver and Start/Stop hooks. Instance.Close only performs provider descriptor cleanup when those hooks were installed by the supported provider/runtime lifecycle. Runtime.Close only requests asynchronous shutdown. CloseContext(context.Background()) or Close followed by WaitClosed is required to observe completed provider teardown and its joined error. Tests and complete-owned-lifecycle measurements use that completion operation. Repeated shutdown uses the existing synchronized closed state; owned files are removed from maps after closing.

Raw bundles are not shared to reduce allocations. Raw commands that leave files open do not acquire a new cleanup guarantee in this performance patch. The complete-owned-lifecycle diagnostic will use the supported Provider lifecycle and will include instance cleanup. Its one-time runtime/plugin/module setup is measured separately. Raw command benchmarks remain unchanged.

The proposed construction optimization changes fixed definition and callback construction only. It does not change fsState, stateFor, closeInstance, closeAll, permissions, context handling, or their locking.
