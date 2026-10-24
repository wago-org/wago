# PR 948: rebuild the bundled RV64 managed dispatcher

The [native RISE job](https://github.com/wago-org/wago/actions/runs/39732958257/job/119672147331)
for old head `72e684d` reports one test failure:
`TestRV64ManagedDispatcherBuiltinMatchesSource`. This is distinct from the
previously fixed stats-fixture build failures and still affects head `1d9fbd472`.

The built-in dispatcher copied from main uses artifact version 6. This branch
uses version 7 and explicitly rejects version 6 under its cached-start contract.
Compiling `managedVoidDispatcherWasm` and marshaling the result produces 2,987
bytes. The only difference from the checked-in image is offset 4: 6 becomes 7.
Every subsequent byte, including native code and metadata, is identical.

Use the existing source-comparison test's
`WAGO_RV64_MANAGED_DISPATCHER_OUTPUT` hook to generate a new image. The test
fails before the update and leaves its generated output in the evidence folder.
Copy that compiler output to the bundled file. Keep the version-7 contract,
unknown-version rejection, cached-start checks, and exact source/artifact byte
comparison unchanged. Do not patch the version in a binary by hand or weaken
the comparison to ignore it.

The old image SHA-256 is
`056b25bd0c2ecdb326ebe645fb44eb4e182e70bffed17d7aee5a460a57dfd51a`.
The generated image SHA-256 is
`4e75f233e518b6c2935e062e678ecc7231a60f1a31ca947e1ec7986c7ad88952`.
The format and file length are unchanged by this regeneration; it synchronizes
the bundled image with the branch's current format version.

## Validation

Linux RV64 QEMU uses the RVA20 baseline with register checks. Two source-mode
repeats cover exact built-in/source equality, current/unknown version contracts,
local/imported/trapping cached starts, managed table dispatch, caller identity,
and retired foreign producers. Prepared fixture artifacts are then loaded in
a compiler-free build, whose managed dispatcher comes from the updated embedded
file. Both gates pass twice. The source-mode command takes 37.969 seconds
including the cross-build, with peak RSS 4,863,224 KiB; its package test phase
takes 0.289 seconds. The compiler-free command takes 29.747 seconds including
the cross-build, with peak RSS 4,326,764 KiB; its package test phase takes
0.181 seconds. Compiler-free source-only cases explicitly skip; the source-mode
gate executes them. The source-mode build exceeds the 30-second compiler-work
budget and remains a build-time cost, not an execution-speed result. The docs
check passes for 100 Markdown files in 0.063 seconds, with peak RSS 25,316 KiB.
QEMU proves these selected execution paths, not native hardware timing. The
next hosted RISE run must qualify the full platform suite. Earlier full native
local and lint gates remain recorded separately; this update changes one
bundled image and this note, not compiler source or test assertions.
