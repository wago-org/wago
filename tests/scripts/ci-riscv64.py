#!/usr/bin/env python3
"""Check native RV64 source lanes and failure propagation with a recording Go tool."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix="wago-rv64-ci-test.") as tmp:
    root = Path(tmp)
    bin_dir = root / "bin"
    bin_dir.mkdir()
    (bin_dir / "uname").write_text('#!/bin/sh\nprintf "riscv64\\n"\n')
    (bin_dir / "go").write_text('''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
if args == ['env', 'GOOS']: print('linux'); sys.exit(0)
if args == ['env', 'GOARCH']: print('riscv64'); sys.exit(0)
if args and args[0] == 'test':
 with open(os.environ['CAPTURE_GO'], 'a') as f: f.write(json.dumps(args)+'\\n')
 sys.exit(int(os.environ.get('TEST_GO_STATUS', '0')))
''')
    for path in bin_dir.iterdir():
        path.chmod(0o755)

    def run(mode, check="all", failure=0, expected=0):
        capture = root / "capture"
        capture.unlink(missing_ok=True)
        env = dict(os.environ, PATH=str(bin_dir)+os.pathsep+os.environ["PATH"],
                   CAPTURE_GO=str(capture), WAGO_RV64_CI_CHECK=check,
                   TEST_GO_STATUS=str(failure))
        result = subprocess.run(["bash", "scripts/ci-riscv64.sh", mode], cwd=repo,
                                env=env, text=True, capture_output=True)
        assert result.returncode == expected, result.stdout + result.stderr
        return [json.loads(line) for line in capture.read_text().splitlines()] if capture.exists() else []

    for mode in ["public", "guards"]:
        ordinary = run(mode, "ordinary")
        checked = run(mode, "checked")
        assert len(ordinary) == len(checked) == 1
        assert run(mode) == ordinary + checked
        assert any("wago_regalloccheck" in arg for arg in checked[0])
    assert len(run("public", failure=43, expected=43)) == 1
    run("public", "invalid", expected=2)
    run("artifacts", expected=2)
    workflow = (repo / ".github/workflows/riscv64.yml").read_text()
    gate = workflow.split("  native:\n", 1)[1]
    assert "needs: qualification" in gate
    body = gate.split("        run: |\n", 1)[1]
    body = "\n".join(line[10:] for line in body.splitlines())
    for status in ["success", "failure", "cancelled", "skipped"]:
        result = subprocess.run(["bash", "-euo", "pipefail", "-c", body],
                                env=dict(os.environ, QUALIFICATION_RESULT=status), capture_output=True)
        assert (result.returncode == 0) == (status == "success")
print("RV64 source lanes and failure propagation passed")
