#!/usr/bin/env python3
"""Independent bounded review at the immutable d6f4e94 source pin."""
import hashlib
import json
import os
import shutil
import subprocess
import tarfile
from pathlib import Path

EVIDENCE = Path(__file__).resolve().parent
LOGS = EVIDENCE / "logs"
LOGS.mkdir(exist_ok=True)
ROOT = Path("/home/dev/.cache/wago-final-codegen-recheck")
SOURCE = ROOT / "source"
PIN = "d6f4e94fe671542d7e22fe84e8fe1f9358661c3a"
REPO = Path("/home/dev/.t3/worktrees/wago/experiment-rfwgc-port")
GO = Path("/home/dev/.local/share/mise/installs/go/1.27.1/bin/go")
QEMU = Path("/tmp/qemu-x/usr/bin/qemu-aarch64")
NODE = Path("/home/dev/.local/share/mise/installs/node/24.22.2/bin")
WABT = Path("/home/dev/.local/share/mise/installs/github-web-assembly-wabt/1.0.42/bin")
BACKEND = "src/core/compiler/backend/railshot/arm64"
records = []


def record(name, argv, cwd, env, timeout=300):
    log = LOGS / f"{name}.log"
    with log.open("wb") as output:
        try:
            rc = subprocess.run(argv, cwd=cwd, env=env, stdout=output,
                                stderr=subprocess.STDOUT, timeout=timeout).returncode
        except subprocess.TimeoutExpired:
            rc = 124
    records.append({
        "name": name, "argv": list(map(str, argv)), "cwd": str(cwd),
        "environment": {key: env.get(key, "") for key in
                        ("GOROOT", "GOWORK", "GOTOOLCHAIN", "GOARCH", "GOOS",
                         "CGO_ENABLED", "GOCACHE", "PATH")},
        "exit": rc, "log": str(log.relative_to(EVIDENCE)),
    })
    (EVIDENCE / "commands.json").write_text(json.dumps({"source": PIN, "commands": records}, indent=2) + "\n")
    print(name, rc, flush=True)
    return rc


ROOT.mkdir(parents=True, exist_ok=True)
archive = ROOT / "source.tar"
with archive.open("wb") as output:
    subprocess.run(["git", "archive", "--format=tar", PIN], cwd=REPO,
                   stdout=output, check=True)
if SOURCE.exists():
    shutil.rmtree(SOURCE)
SOURCE.mkdir()
with tarfile.open(archive) as source_tar:
    source_tar.extractall(SOURCE, filter="data")

base_env = {key: value for key, value in os.environ.items() if not key.startswith("WAGO_")}
base_env.update({
    "GOROOT": str(GO.parent.parent), "GOWORK": "off", "GOTOOLCHAIN": "local",
    "GOARCH": "arm64", "GOOS": "linux", "CGO_ENABLED": "0",
    "GOCACHE": str(ROOT / "gocache"),
    "PATH": f"{NODE}:{WABT}:/home/dev/.local/bin:/usr/local/bin:/usr/bin:/bin",
})

record("go-version", [GO, "version"], SOURCE, base_env)
record("qemu-version", [QEMU, "--version"], SOURCE, base_env)
record("node-version", [NODE / "node", "--version"], SOURCE, base_env)
record("wabt-version", [WABT / "wast2json", "--version"], SOURCE, base_env)

focused = ("^(TestNativeImportVariantRoutesARM64|TestDynamicImportUnwindLocalJoinARM64|"
           "TestNativeLeafContextShortcutGCPolicyARM64|TestTable.*Lowering|"
           "TestBulkPhysicalArgumentSlots|TestTableSetHelperCallHintsArm64|"
           "TestWorkerModuleStatsComparisonARM64|TestConstFoldPackExecArm64|"
           "TestSameOperandCompareExecArm64)$")
diagnostics = ("^(TestCodegenStatsPeepholesArm64|TestCompactShiftedAddImmediateArm64|"
               "TestConstFoldPackPeepArm64|TestEntryArgumentRegistersPromoteLeafLocalsArm64|"
               "TestEntryInitializedLocalSkipsZeroArm64|TestEntryParamPairsWrapperARM64|"
               "TestEntryZeroPairsARM64|TestIntervalRegionDynamicReuseArm64|"
               "TestLeafScratchPinAssignmentsArm64|TestZeroBranchIfArm64|"
               "TestSharedEstablishedPathPairs|TestSharedScalarLeafMatchesEstablishedCodeArm64)$")
public = ("^(TestNativeImportVariantSourceArtifactAndCalls|TestNativeImportVariantTrap|"
          "TestNativeImportVariantRespectsPolicies|TestNativeImportVariantForeignFallback)$")

for mode in ("ordinary", "wagodebug", "wago_regalloccheck"):
    for lane, package, selector, extra in (
        ("focused", BACKEND, focused, ""),
        ("diagnostics", BACKEND, diagnostics, "wago_codegenstats"),
        ("public", "src/wago", public, ""),
    ):
        tags = ",".join(x for x in (("" if mode == "ordinary" else mode), extra) if x)
        binary = ROOT / f"{mode}-{lane}.test"
        if record(f"{mode}-{lane}-build", [GO, "test", f"-tags={tags}", "-c", "-o", binary, f"./{package}"], SOURCE, base_env) == 0:
            record(f"{mode}-{lane}", [QEMU, binary, "-test.run", selector, "-test.v", "-test.count=1", "-test.timeout=120s"], SOURCE / package, base_env)

probe_env = dict(base_env)
probe_env.update(GOARCH="amd64", GOOS="linux")
record("branch19-repatch-probe", [GO, "run", EVIDENCE / "_probes/branch19_repatch.go"], SOURCE, probe_env)

negative_compile = ROOT / "negative-compile.go"
with negative_compile.open("wb") as output:
    subprocess.run(["git", "show", "58fbb7d4:src/core/compiler/backend/railshot/arm64/compile.go"], cwd=REPO, stdout=output, check=True)
overlay = ROOT / "negative-overlay.json"
overlay.write_text(json.dumps({"Replace": {str(SOURCE / BACKEND / "compile.go"): str(negative_compile)}}))
negative = ROOT / "negative.test"
if record("negative-build", [GO, "test", f"-overlay={overlay}", "-c", "-o", negative, f"./{BACKEND}"], SOURCE, base_env) == 0:
    record("negative", [QEMU, negative, "-test.run", "^TestNativeImportVariantRoutesARM64$/compact=true/pair=false/trap=false", "-test.v", "-test.count=1"], SOURCE / BACKEND, base_env)

hashes = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(ROOT.glob("*.test"))}
(EVIDENCE / "binary-hashes.json").write_text(json.dumps(hashes, indent=2) + "\n")
(EVIDENCE / "source-integrity.json").write_text(json.dumps({
    "source": PIN,
    "tree": subprocess.check_output(["git", "rev-parse", f"{PIN}^{{tree}}"], cwd=REPO, text=True).strip(),
    "archive_sha256": hashlib.sha256(archive.read_bytes()).hexdigest(),
    "export": str(SOURCE),
}, indent=2) + "\n")
