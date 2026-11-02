#!/usr/bin/env python3
"""Immutable, correctness-only recheck for unsupported global conversion guards."""

import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile


ROOT = pathlib.Path(__file__).resolve().parents[3]
OUT = pathlib.Path(__file__).resolve().parent
GO = pathlib.Path("/home/dev/.local/share/mise/installs/go/1.27.1/bin/go")
PINS = {
    "source": "7e41fafbbcd737ed1edce11b94b43b4eda134dcf",
    "review_evidence": "8526d061047dd4b6afd218157a5e4c709456a167",
    "previous": "3242c18a19620aa77f39dd63cb5cd7544d4ca70a",
}
MODES = {
    "ordinary": "wago_target_tinygo",
    "wagodebug": "wago_target_tinygo,wagodebug",
    "wago_regalloccheck": "wago_target_tinygo,wago_regalloccheck",
}
PROBE = r'''//go:build wago_target_tinygo && !riscv64

package wago

import (
	"encoding/binary"
	"testing"
)

func TestReviewImportedAccessNamespace(t *testing.T) {
	store := newReferenceStore(false)
	const public = uint64(0x1234567800000042)
	conversion := &gcExternConversionState{store: store, entries: []gcExternConversionEntry{{kind: gcExternConversionData, publicExternWord: public}}}
	access := &Instance{refStore: store}
	state := &instancePluginState{}
	state.gcRefTestTable.Store(&gcRefTestTableState{Conversion: conversion})
	access.pluginState.Store(state)
	physical := &Instance{refStore: store}
	cell := make([]byte, 8)
	binary.LittleEndian.PutUint64(cell, 99)
	g := &Global{Type: ValExternRef, Mutable: true, cell: cell, owner: &globalOwner{instance: physical, store: store, typ: ValExternRef, mutable: true}}
	handled, err := setConversionGlobalPublic(access, g, Value{typ: ValExternRef, bits: public})
	if !handled || err == nil {
		t.Fatalf("access namespace was not rejected: handled=%t err=%v", handled, err)
	}
	if got := binary.LittleEndian.Uint64(cell); got != 99 {
		t.Fatalf("rejection changed cell: %d", got)
	}
}
'''


def sha256(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def archive(pin, destination):
    tar = destination.parent / (destination.name + ".tar")
    with tar.open("wb") as f:
        subprocess.run(["git", "archive", "--format=tar", pin], cwd=ROOT, stdout=f, check=True)
    digest = sha256(tar)
    destination.mkdir()
    subprocess.run(["tar", "-xf", str(tar), "-C", str(destination)], check=True)
    tar.unlink()
    return digest


def run(name, source, args, env, expect):
    log = OUT / (name + ".log")
    cmd = [str(GO), "test", *args, "-count=1", "-v", "-timeout=2m"]
    with log.open("w") as f:
        code = subprocess.run(cmd, cwd=source, env=env, stdout=f, stderr=subprocess.STDOUT).returncode
    row = {"name": name, "command": cmd, "expected_exit": expect, "exit": code}
    if (code == 0) != (expect == 0):
        row["unexpected"] = True
    return row


def main():
    env = dict(os.environ, GOROOT=str(GO.parent.parent), GOWORK="off", GOMAXPROCS="4")
    rows = []
    manifests = {}
    with tempfile.TemporaryDirectory(prefix="wago-unsupported-global-recheck-") as td:
        temp = pathlib.Path(td)
        source = temp / "source"
        manifests["source_archive_sha256"] = archive(PINS["source"], source)
        manifests["source_files"] = {
            path: sha256(source / path)
            for path in [
                "src/wago/gc_conversion_store_other.go",
                "src/wago/gc_conversion_unsupported_test.go",
            ]
        }
        regex = "^TestUnsupportedConversion"
        for mode, tags in MODES.items():
            rows.append(run(mode, source, ["-tags=" + tags, "./src/wago", "-run", regex], env, 0))
            rows.append(run(mode + "-race", source, ["-race", "-tags=" + tags, "./src/wago", "-run", regex], env, 0))

        previous = temp / "previous"
        manifests["previous_archive_sha256"] = archive(PINS["previous"], previous)
        probe = previous / "src/wago/review_imported_access_namespace_test.go"
        probe.write_text(PROBE)
        manifests["negative_control_probe_sha256"] = sha256(probe)
        rows.append(run("previous-negative-control", previous, ["-tags=wago_target_tinygo", "./src/wago", "-run", "^TestReviewImportedAccessNamespace$"], env, 1))

    tool = {"path": str(GO), "sha256": sha256(GO), "goroot": str(GO.parent.parent)}
    (OUT / "source.json").write_text(json.dumps({"pins": PINS, **manifests}, indent=2) + "\n")
    (OUT / "tool.json").write_text(json.dumps(tool, indent=2) + "\n")
    (OUT / "results.json").write_text(json.dumps(rows, indent=2) + "\n")
    bad = [row for row in rows if row.get("unexpected")]
    print(json.dumps({"rows": len(rows), "unexpected": len(bad)}, indent=2))
    return bool(bad)


if __name__ == "__main__":
    sys.exit(main())
