"""Compare bounded loop layouts after a single exact-oracle gate per layout."""
import os
import pathlib
import subprocess
import time
import queued_policy_probe as probe

probe.BINARY = "/tmp/arm64-parity-loop-layout.test"
probe.VARIANTS = {
    name: {"WAGO_ARM64_EXPERIMENT_LOOP_LAYOUT": name}
    for name in ("phase0", "phase8", "align32", "align64")
}
for name, settings in probe.VARIANTS.items():
    env = dict(os.environ, **settings)
    env["WAGO_PARITY_CORPUS"] = str((probe.ROOT / "../../Web/wasm.fyi/corpora/applications").resolve())
    with (probe.OUT / f"loop-layout-{name}-oracles.txt").open("w") as output:
        subprocess.run(["python3", str(pathlib.Path.home() / "benchmark-lock.py"),
                        "--wait", "--owner", "wago-root", "--", probe.BINARY,
                        "-test.run", "^$", "-test.bench", "BenchmarkParity/.*/Exec$",
                        "-test.benchtime=1x"], cwd=probe.ROOT, env=env,
                       stdout=output, stderr=subprocess.STDOUT, check=True)
    time.sleep(10)
for round_number in range(2):
    items = list(probe.VARIANTS.items())
    if round_number:
        items.reverse()
    for name, settings in items:
        probe.run("layout-base-before-" + name, {}, round_number)
        probe.run("layout-" + name, settings, round_number)
