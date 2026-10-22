#!/usr/bin/env python3
"""Prepare pinned Release 2/3 fixtures without fetching tools or source.

Usage: scripts/prepare-riscv64-numeric-spec.py [--profile numeric|float|memory|control|multimemory|memory64|fac|call] SPEC_CHECKOUT OUTPUT_DIRECTORY WAST2JSON
The default numeric profile preserves the original four-file corpus.
Use the existing WABT 1.0.41 binary bootstrapped by scripts/bootstrap-wabt.sh.
The output directory must not exist. Generation has a 30-second limit per
subprocess and a 1 GiB address-space ceiling for each WABT process.
"""
import argparse
import hashlib
import json
import pathlib
import resource
import subprocess
import tempfile

PROFILES = {
    "fac": ("fac.wast",),
    "call": ("call.wast",),
    "numeric": ("conversions.wast", "i32.wast", "i64.wast", "int_exprs.wast"),
    "float": ("f32.wast", "f64.wast", "f32_bitwise.wast", "f64_bitwise.wast", "f32_cmp.wast", "f64_cmp.wast"),
    "memory": ("load.wast", "store.wast", "address.wast", "memory_trap.wast", "endianness.wast"),
    "control": ("local_get.wast", "local_set.wast", "local_tee.wast", "nop.wast", "block.wast", "loop.wast", "br.wast", "br_if.wast", "if.wast", "return.wast"),
    "multimemory": ("load0.wast", "load2.wast", "address0.wast", "address1.wast", "memory_trap0.wast", "memory_trap1.wast"),
    "memory64": ("endianness64.wast", "address64.wast", "load64.wast", "memory_trap64.wast"),
}
PIN = "05ca4182176763112561ae20153975c12bd689e4"
R3_PIN = "9d36019973201a19f9c9ebb0f10828b2fe2374aa"
ARCHIVES = {
    "83f8122e924745fcd70636e3594bc01c4c47f2d4c8f3c63b5d70d3f83a482677",
    "5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf",
    "e5269d6bbe05dfeb179e4f21111b3a641d6ccaa38b0b21d472ae5c65f8c4ff5d",
    "ca9e69cc1de13b4633a3c74fd697319303b21108529d4f10960af4e1f4a65893",
}


def run(*args):
    return subprocess.check_output(args, timeout=30)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def hash_rows(rows):
    return digest("".join(name + "\0" + rows[name] + "\n" for name in sorted(rows)).encode())


def bound_child():
    resource.setrlimit(resource.RLIMIT_AS, (1 << 30, 1 << 30))
    resource.setrlimit(resource.RLIMIT_FSIZE, (16 << 20, 16 << 20))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=PROFILES, default="numeric")
    parser.add_argument("source")
    parser.add_argument("output")
    parser.add_argument("wast2json")
    args = parser.parse_args()
    repo = pathlib.Path(__file__).resolve().parent.parent
    source, output, wabt = (pathlib.Path(arg).resolve() for arg in (args.source, args.output, args.wast2json))
    release, pin, family, flags = "spec-v2", PIN, "", []
    if args.profile in ("multimemory", "memory64"):
        release, pin = "spec-v3", R3_PIN
        family = "multi-memory" if args.profile == "multimemory" else "memory64"
        flags = ["--enable-all"]
    recorded = run("git", "-C", str(repo), "ls-tree", "HEAD", "tests/conformance/" + release).decode().split()[2]
    if recorded != pin or run("git", "-C", str(source), "rev-parse", "HEAD").decode().strip() != pin:
        raise SystemExit("source checkout or repository pin differs from the numeric suite pin")
    if output.exists():
        raise SystemExit("output directory already exists; refusing to overwrite fixtures")
    stamps = dict(line.split("=", 1) for line in (wabt.parent.parent / ".wago-provenance").read_text().splitlines())
    version = run(str(wabt), "--version").decode().strip()
    if version != "1.0.41" or stamps.get("version") != version or stamps.get("archive_sha256") not in ARCHIVES or stamps.get("binary_sha256") != digest(wabt.read_bytes()):
        raise SystemExit("existing WABT binary or provenance differs from pinned WABT")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".rv64-numeric-", dir=output.parent) as tmp:
        stage = pathlib.Path(tmp) / "fixtures"
        stage.mkdir()
        sources = {}
        for name in PROFILES[args.profile]:
            fixture = source / "test/core" / family / name
            relative = fixture.relative_to(source).as_posix()
            committed = run("git", "-C", str(source), "show", pin + ":" + relative)
            if fixture.read_bytes() != committed:
                raise SystemExit("modified pinned source: " + relative)
            sources[name] = digest(committed)
            target = stage / (fixture.stem + ".json")
            subprocess.run([str(wabt), *flags, str(fixture), "-o", str(target)], check=True, timeout=30, preexec_fn=bound_child)
            document = json.loads(target.read_text())
            document["source_filename"] = name
            target.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
        files = {}
        total = 0
        for artifact in sorted(stage.iterdir()):
            size = artifact.stat().st_size
            total += size
            if size > 16 << 20 or total > 32 << 20 or not artifact.is_file():
                raise SystemExit("generated fixture exceeds bounded corpus size")
            files[artifact.name] = digest(artifact.read_bytes())
        manifest = {"spec_commit": pin, "wabt_version": version,
                    "wabt_archive_sha256": stamps["archive_sha256"],
                    "wabt_binary_sha256": stamps["binary_sha256"],
                    "sources": sources, "files": files}
        (stage / ("rv64-" + args.profile + "-manifest.json")).write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        stage.rename(output)
    print("Prepared", len(sources), "sources,", len(files), "files,", total, "bytes")
    print("sources_sha256", hash_rows(sources))
    print("files_sha256", hash_rows(files))


if __name__ == "__main__":
    main()
