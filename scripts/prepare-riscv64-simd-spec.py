#!/usr/bin/env python3
"""Prepare pinned Release 2 SIMD fixtures for the private RV64 test harness.

Usage: scripts/prepare-riscv64-simd-spec.py SPEC_CHECKOUT OUTPUT_DIRECTORY
       scripts/prepare-riscv64-simd-spec.py --relaxed SPEC_CHECKOUT OUTPUT_DIRECTORY
Requires an existing checkout; never updates submodules or fetches source.
The output directory must not exist. Run the RV64 backend test binary with
WAGO_RV64_SPEC_JSON_DIR pointing to the resulting directory.
For Release 3 relaxed fixtures, set WAGO_RV64_RELAXED_SPEC_JSON_DIR instead.
"""
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    relaxed = len(sys.argv) > 1 and sys.argv[1] == "--relaxed"
    if relaxed:
        sys.argv.pop(1)
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    repo = pathlib.Path(__file__).resolve().parent.parent
    source, output = (pathlib.Path(x).resolve() for x in sys.argv[1:])
    release = "spec-v3" if relaxed else "spec-v2"
    pin = run("git", "-C", str(repo), "ls-tree", "HEAD", "tests/conformance/" + release).split()[2]
    if run("git", "-C", str(source), "rev-parse", "HEAD") != pin:
        raise SystemExit("spec checkout differs from repository's " + release + " pin")
    if output.exists():
        raise SystemExit("output directory already exists; refusing to overwrite fixtures")
    wabt = pathlib.Path(run(str(repo / "scripts/bootstrap-wabt.sh"), "--print-path"))
    version = run(str(wabt), "--version")
    stamps = dict(line.split("=", 1) for line in (wabt.parent.parent / ".wago-provenance").read_text().splitlines())
    fixture_dir = "test/core/relaxed-simd" if relaxed else "test/core/simd"
    fixtures = sorted((source / fixture_dir).glob("*.wast"))
    if len(fixtures) != (7 if relaxed else 57):
        raise SystemExit("unexpected pinned SIMD fixture count")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".rv64-simd-", dir=output.parent) as tmp:
        stage = pathlib.Path(tmp) / "fixtures"
        stage.mkdir()
        hashes, source_hashes = {}, {}
        for fixture in fixtures:
            relative = fixture.relative_to(source).as_posix()
            committed = subprocess.check_output(["git", "-C", str(source), "show", pin + ":" + relative])
            if fixture.read_bytes() != committed:
                raise SystemExit("modified pinned source: " + relative)
            source_hashes[fixture.name] = hashlib.sha256(committed).hexdigest()
            document_path = stage / (fixture.stem + ".json")
            flags = ["--enable-relaxed-simd"] if relaxed else []
            subprocess.run([str(wabt), *flags, str(fixture), "-o", str(document_path)], check=True)
            # The only path-dependent output field is diagnostic metadata.
            # Normalize it before recording hashes so all hosts reproduce bytes.
            document = json.loads(document_path.read_text())
            document["source_filename"] = fixture.name
            document_path.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
        for artifact in sorted(stage.iterdir()):
            hashes[artifact.name] = digest(artifact)
        manifest = {"spec_commit": pin, "wabt_version": version,
                    "wabt_archive_sha256": stamps["archive_sha256"],
                    "wabt_binary_sha256": stamps["binary_sha256"],
                    "sources": source_hashes, "files": hashes}
        (stage / "rv64-simd-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        stage.rename(output)
    print("Prepared", len(fixtures), "pinned SIMD fixtures at", output)
    print("spec", pin, "wast2json", version, "archive", stamps["archive_sha256"])


if __name__ == "__main__":
    main()
