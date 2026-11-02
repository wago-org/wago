#!/usr/bin/env python3
"""Independent correctness checks against an immutable source archive."""

import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tarfile
import tempfile


PIN = "3242c18a19620aa77f39dd63cb5cd7544d4ca70a"
ROOT = pathlib.Path(__file__).resolve().parents[3]
OUT = pathlib.Path(__file__).resolve().parent
GO = pathlib.Path("/home/dev/.local/share/mise/installs/go/1.27.1/bin/go")
FOCUSED = "^(TestConverted.*|TestExternrefGlobalHoldingConvertedGCReference|TestR9H2.*|TestR9Ownership.*|TestManagedFunctionHostShape)$"
UNSUPPORTED = "^TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory$"


def run(argv, cwd, env, log):
    with (OUT / log).open("w") as stream:
        result = subprocess.run(argv, cwd=cwd, env=env, stdout=stream,
                                stderr=subprocess.STDOUT, timeout=900)
    return {"command": argv, "cwd": str(cwd), "exit": result.returncode,
            "log": log}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    archive = OUT / "source.tar"
    with archive.open("wb") as stream:
        subprocess.run(["git", "archive", "--format=tar", PIN], cwd=ROOT,
                       stdout=stream, check=True)
    archive_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
    temp = pathlib.Path(tempfile.mkdtemp(prefix="wago-darwin-review-"))
    source = temp / "source"
    source.mkdir()
    with tarfile.open(archive) as bundle:
        bundle.extractall(source, filter="data")
    archive.unlink()

    env = dict(os.environ)
    env.update({
        "GOROOT": str(GO.parent.parent),
        "GOWORK": "off",
        "GOMAXPROCS": "4",
        "PATH": ":".join([
            str(GO.parent),
            "/home/dev/.local/share/mise/installs/github-web-assembly-wabt/1.0.42/bin",
            "/home/dev/.local/share/mise/installs/github-bytecodealliance-wasm-tools/1.251.0",
            "/home/dev/.local/share/mise/installs/node/24.22.2/bin",
            env["PATH"],
        ]),
    })
    rows = []
    for mode in ("ordinary", "wagodebug", "wago_regalloccheck"):
        tags = [] if mode == "ordinary" else ["-tags=" + mode]
        rows.append(run([str(GO), "test", *tags, "./src/wago", "-run", FOCUSED,
                         "-count=1", "-timeout=5m", "-v"], source, env,
                        mode + "-focused.log"))
        rows.append(run([str(GO), "test", *tags, "-race", "./src/wago", "-run", FOCUSED,
                         "-count=1", "-timeout=8m", "-v"], source, env,
                        mode + "-race.log"))
        unsupported_tags = ["wago_target_tinygo"]
        if mode != "ordinary":
            unsupported_tags.append(mode)
        rows.append(run([str(GO), "test", "-tags=" + ",".join(unsupported_tags),
                         "./src/wago", "-run", UNSUPPORTED, "-count=1",
                         "-timeout=2m", "-v"], source, env,
                        mode + "-unsupported.log"))
        darwin_env = dict(env, GOOS="darwin", GOARCH="arm64", CGO_ENABLED="0")
        output = temp / (mode + "-darwin-wago.test")
        rows.append(run([str(GO), "test", *tags, "-c", "./src/wago", "-o", str(output)],
                        source, darwin_env, mode + "-darwin-cross.log"))

    for name, target_env, tags in (
        ("darwin-selection", {"GOOS": "darwin", "GOARCH": "arm64", "CGO_ENABLED": "0"}, []),
        ("linux-amd64-selection", {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}, []),
        ("linux-rv64-selection", {"GOOS": "linux", "GOARCH": "riscv64", "CGO_ENABLED": "0"}, []),
        ("tinygo-tag-selection", {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}, ["wago_target_tinygo"]),
    ):
        selected_env = dict(env, **target_env)
        argv = [str(GO), "list", "-json"]
        if tags:
            argv.append("-tags=" + ",".join(tags))
        argv.extend(["./src/wago", "./src/core/runtime/gc/native"])
        rows.append(run(argv, source, selected_env, name + ".json"))

    provenance = {
        "pin": PIN,
        "archive_sha256": archive_hash,
        "go": str(GO),
        "go_sha256": hashlib.sha256(GO.read_bytes()).hexdigest(),
        "goroot": env["GOROOT"],
        "temporary_source_removed": True,
    }
    (OUT / "results.json").write_text(json.dumps(rows, indent=2) + "\n")
    (OUT / "provenance.json").write_text(json.dumps(provenance, indent=2) + "\n")
    shutil.rmtree(temp)
    return 1 if any(row["exit"] for row in rows) else 0


if __name__ == "__main__":
    sys.exit(main())
