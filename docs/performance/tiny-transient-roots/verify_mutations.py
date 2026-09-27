#!/usr/bin/env python3
"""Check that Tiny root/restart regressions fail, without editing the worktree."""

import json
from pathlib import Path
import re
import subprocess
import tempfile


REPO = Path(__file__).resolve().parents[3]
NATIVE = "src/core/runtime/gc/native/"
BEFORE_PR = "30919ef8c5e993ed4dcea7af4c2b580e01dbec21"
BEFORE_STAGING = "1180bab1d56ae90d75ffff913e596b3ec8829030"


def historical_sources(revision):
    return {
        NATIVE + name: subprocess.check_output(
            ["git", "show", f"{revision}:{NATIVE}{name}"], cwd=REPO, text=True
        )
        for name in ("mark.go", "tiny_collect.go")
    }


def main():
    source = (REPO / NATIVE / "tiny_collect.go").read_text()
    clear = "\t\tclear(c.tinyGC.color)"
    if source.count(clear) != 2:
        raise SystemExit("expected both incremental and synchronous restart guards")
    no_clear = source.replace(clear, "\t\t// Mutation: keep stale epoch marks.")
    advance, replacements = re.subn(
        r"\tif c.tinyGC.state != tinyIdle \{\n.*?\n\t\} else \{\n"
        r"\t\tc.tinyGC.markEpoch = \(c.tinyGC.markEpoch \+ 1\) & tinyMarkEpochMask\n\t\}",
        "\tc.tinyGC.markEpoch = (c.tinyGC.markEpoch + 1) & tinyMarkEpochMask",
        source,
        flags=re.S,
    )
    if replacements != 2:
        raise SystemExit("expected both restart epoch branches")
    cases = [
        ("double-read", historical_sources(BEFORE_PR), [
            "TestTinyKeepsOneShotDirectRoot",
            "TestTinyOneShotRootsKeepGraphAndReleaseNextCycle",
        ]),
        ("discarded-marks", historical_sources(BEFORE_STAGING), [
            "TestTinyFailedRootEnumerationPreservesCycle",
            "TestTinyRejectedCompositeRootsDoNotPublishPartialMarks",
        ]),
        ("no-clear", {NATIVE + "tiny_collect.go": no_clear}, [
            "TestTinyRecoveryFromEveryCompletedEpoch",
            "TestTinyFailedRestartsDoNotAliasWrappedEpoch",
        ]),
        ("advance-on-restart", {NATIVE + "tiny_collect.go": advance}, [
            "TestTinyCompletedWrapThenFailedRestartsKeepReachableChild",
            "TestTinyFailedRestartsDoNotAliasWrappedEpoch",
        ]),
    ]
    with tempfile.TemporaryDirectory(prefix="wago-tiny-mutations-") as temp:
        temp = Path(temp)
        for name, sources, expected in cases:
            replacements = {}
            for index, (filename, contents) in enumerate(sources.items()):
                path = temp / f"{name}-{index}.go"
                path.write_text(contents)
                replacements[str(REPO / filename)] = str(path)
            overlay = temp / f"{name}.json"
            overlay.write_text(json.dumps({"Replace": replacements}))
            for tags in ("", "wago_tiny_nonincremental"):
                command = ["go", "test", "-count=1", f"-overlay={overlay}"]
                if tags:
                    command.append(f"-tags={tags}")
                command += ["-run", "^(" + "|".join(expected) + ")$", "./" + NATIVE]
                result = subprocess.run(command, cwd=REPO, text=True,
                                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
                failed = set(re.findall(r"^--- FAIL: (\w+)", result.stdout, re.M))
                if result.returncode != 1 or not set(expected).issubset(failed):
                    print(result.stdout)
                    raise SystemExit(f"{name}/{tags or 'default'} did not fail the expected assertions")
                print(f"{name}/{tags or 'default'}: caught by {', '.join(sorted(failed))}", flush=True)


if __name__ == "__main__":
    main()
