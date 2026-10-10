#!/usr/bin/env python3
"""Serial, matched production-input and executable comparison; no benchmark."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument("baseline", type=Path)
parser.add_argument("candidate", type=Path)
parser.add_argument("output", type=Path)
parser.add_argument("--cgo", choices=("0", "1"), default="0")
parser.add_argument("--native-only", action="store_true")
parser.add_argument("--flavor", action="append")
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
env = os.environ.copy()
env.update(GOMAXPROCS="1", CGO_ENABLED=args.cgo, GOFLAGS="")
platforms = [(system, arch) for system in ("linux", "darwin", "windows")
             for arch in ("amd64", "arm64")]
if args.native_only:
    platforms = [("linux", "amd64")]
flavors = {
    "manager": ("", "./cli/wago"),
    "runtime": ("wago_runtime", "./cli/wago"),
    "minimal": ("wago_runtime,wago_lean,wago_minimal", "./cli/wago"),
    "release-minimal": ("wago_runtime,wago_minimal", "./cli/wago"),
    "embed": ("", "./scripts/testdata/diagnostic-dce"),
}
if args.flavor:
    flavors = {name: flavors[name] for name in args.flavor}
extra_tags = ("wago_profile", "wago_codegenstats",
              "wago_runtime,wago_profile", "wago_runtime,wago_codegenstats",
              "wago_nativecompare,wago_runtime")
results = {"baseline": str(args.baseline.resolve()),
           "candidate": str(args.candidate.resolve()),
           "cgo_enabled": args.cgo,
           "go_version": subprocess.check_output(["go", "version"], text=True).strip(),
           "production_inputs": [], "discovery": [], "binaries": []}


def save():
    (args.output / "results.json").write_text(json.dumps(results, indent=2) + "\n")


def run(repo, command, target_env):
    p = subprocess.run(command, cwd=repo, env=target_env,
                       capture_output=True, text=True)
    if p.returncode:
        raise RuntimeError(f"{repo}: {command}: {p.stderr}")
    return p.stdout


def objects(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        item, end = decoder.raw_decode(text)
        yield item
        text = text[end:]


def manifest(repo, tags, target_env):
    packages = list(objects(run(repo, ["go", "list", "-deps", "-json",
                                      "-tags=" + tags, "./cli/wago", "."], target_env)))
    inputs = []
    for package in packages:
        path = package["ImportPath"]
        if "/tests/tools/native-compare" in path:
            raise RuntimeError("diagnostic linked into production dependency graph")
        files = {}
        for key in ("GoFiles", "CgoFiles", "SFiles", "CFiles", "HFiles", "SysoFiles", "EmbedFiles"):
            for name in package.get(key, []):
                files[name] = hashlib.sha256((Path(package["Dir"]) / name).read_bytes()).hexdigest()
        inputs.append({"path": path, "imports": sorted(package.get("Imports", [])),
                       "files": files})
    encoded = json.dumps(sorted(inputs, key=lambda p: p["path"]), sort_keys=True).encode()
    return hashlib.sha256(encoded).hexdigest(), len(inputs)


for system, arch in platforms:
    target_env = dict(env, GOOS=system, GOARCH=arch)
    for tags in dict.fromkeys([value[0] for value in flavors.values()] + list(extra_tags)):
        before, count = manifest(args.baseline, tags, target_env)
        after, after_count = manifest(args.candidate, tags, target_env)
        if (before, count) != (after, after_count):
            raise RuntimeError(f"production inputs changed: {system}/{arch} {tags}")
        results["production_inputs"].append({"platform": system + "/" + arch,
                                             "tags": tags, "sha256": after,
                                             "packages": count, "identical": True})
        save()
    for tags in ("", "wago_runtime", "wago_profile", "wago_codegenstats"):
        packages = run(args.candidate, ["go", "list", "-tags=" + tags, "./..."], target_env).splitlines()
        if any(p.endswith("/tests/tools/native-compare") for p in packages):
            raise RuntimeError("diagnostic present without explicit opt-in")
        results["discovery"].append({"platform": system + "/" + arch,
                                     "tags": tags, "diagnostic_excluded": True})
        save()
    print(f"{system}/{arch}: production inputs identical; ordinary diagnostic discovery excluded", flush=True)

for system, arch in platforms:
    target_env = dict(env, GOOS=system, GOARCH=arch)
    for flavor, (tags, package) in flavors.items():
        paths = [args.output / (system + "-" + arch + "-" + flavor + "-" + side)
                 for side in ("before", "after")]
        for repo, path in zip((args.baseline, args.candidate), paths):
            run(repo, ["go", "build", "-p=1", "-trimpath", "-buildvcs=false",
                       "-tags=" + tags,
                       "-ldflags=-buildid= -X main.version=production-isolation-control",
                       "-o", str(path.resolve()), package], target_env)
        hashes = [hashlib.sha256(p.read_bytes()).hexdigest() for p in paths]
        if hashes[0] != hashes[1]:
            raise RuntimeError(f"executable changed: {system}/{arch} {flavor}")
        symbols = run(args.candidate, ["go", "tool", "nm", str(paths[1].resolve())], target_env)
        markers = ("tests/tools/native-compare", " main.Compare", " main.captureBytes",
                   " main.inlineRoots", " main.populateRaw", " main.populateRegions",
                   " main.sourceOwnerFunction")
        if any(marker in symbols for marker in markers):
            raise RuntimeError("diagnostic symbol retained in production executable")
        results["binaries"].append({"platform": system + "/" + arch,
                                   "flavor": flavor, "tags": tags,
                                   "bytes": paths[1].stat().st_size,
                                   "sha256": hashes[1], "identical": True,
                                   "diagnostic_symbols_absent": True})
        save()
        print(f"{system}/{arch} {flavor}: byte-identical; diagnostic symbols absent", flush=True)
        # Keep native candidates for bounded execution; hashes preserve cross evidence.
        paths[0].unlink()
        if (system, arch) != ("linux", "amd64"):
            paths[1].unlink()
results["completed"] = True
save()
print("PASS: production inputs, ordinary discovery and matched executables", flush=True)
