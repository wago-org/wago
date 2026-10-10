"""Small alternating policy probes; each batch releases the shared device lock."""
import os
import pathlib
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "experiments/arm64-parity"
BINARY = "/tmp/arm64-parity-queued-current.test"
CASES = "search-aho-corasick|graphics-reed-solomon|ml-inference|vision-dilation|video-dct|crypto-aes128"
VARIANTS = {
    "loop-entry-off": {"WAGO_ARM64_NO_CALLFREE_LOOP_ENTRY": "1"},
    "loop-region-off": {"WAGO_ARM64_NO_CALLFREE_LOOP_REGION": "1"},
    "constant-cache-off": {"WAGO_ARM64_NO_LOOP_INT_CONST": "1"},
    "indexed-reuse-off": {"WAGO_ARM64_NO_INDEXED_BASE_REUSE": "1"},
    "load-pair-off": {"WAGO_ARM64_NO_LOAD_PAIR": "1"},
    "compact": {"WAGO_COMPACT": "1"},
}


def run(label, settings, round_number):
    env = dict(os.environ)
    for values in VARIANTS.values():
        for key in values:
            env.pop(key, None)
    env.update(settings)
    env["WAGO_PARITY_CORPUS"] = str((ROOT / "../../Web/wasm.fyi/corpora/applications").resolve())
    path = OUT / f"policy-probe-{round_number}-{label}.txt"
    cmd = ["python3", str(pathlib.Path.home() / "benchmark-lock.py"),
           "--wait", "--owner", "wago-root", "--", BINARY,
           "-test.run", "^$", "-test.bench", f"BenchmarkParity/({CASES})/(Compile|Exec)$",
           "-test.benchtime=150ms", "-test.count=2"]
    print(f"running {path.name}", flush=True)
    with path.open("w") as output:
        subprocess.run(cmd, cwd=ROOT, env=env, stdout=output,
                       stderr=subprocess.STDOUT, check=True)
    time.sleep(10)


if __name__ == "__main__":
    for round_number in range(2):
        items = list(VARIANTS.items())
        if round_number:
            items.reverse()
        for label, settings in items:
            run("base-before-" + label, {}, round_number)
            run(label, settings, round_number)
