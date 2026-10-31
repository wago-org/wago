"""Exercise evidence handling without building or timing the compiler."""
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "compare_cold", Path(__file__).with_name("compare-cold.py"))
cold = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cold)


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.output = self.root / "evidence"
        self.output.mkdir()
        self.manifest = self.root / "manifest.json"
        self.manifest.write_text(json.dumps({"corpus": [{"id": "fixture", "sha256": "hash"}]}))
        for rel in ("bench/suite/optimization_study_test.go", "src/wago/compile_review_bench_test.go"):
            target = self.root / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text("package fixture\n")
        self.dirty = ""
        self.failure = None
        for target, value in (("ROOT", self.root), ("OUTPUT", self.output), ("MANIFEST", self.manifest)):
            self.enterContext(patch.object(cold, target, value))
        self.enterContext(patch.object(cold.platform, "system", return_value="Darwin"))
        self.enterContext(patch.object(cold.platform, "machine", return_value="arm64"))
        uname = self.enterContext(patch.object(cold.platform, "uname"))
        uname.return_value._asdict.return_value = {"system": "Darwin"}
        self.enterContext(patch.object(cold, "run", side_effect=self.run_command))
        self.commands = self.enterContext(patch.object(cold.subprocess, "run", side_effect=self.build_command))
        self.enterContext(patch("sys.stdout", new_callable=io.StringIO))

    def run_command(self, args, **kwargs):
        if args[:3] == ["go", "env", "GOARCH"]:
            return "arm64"
        if args[:2] == ["git", "status"]:
            return self.dirty
        if args[0] == "/bin/sh":
            if self.failure:
                raise self.failure
            if "-test.run=^$" in args:
                return "BenchmarkCompileSmallModule 10000 8000 ns/op"
            row = {"id": "fixture", "phase": kwargs["env"]["WAGO_STUDY_PHASE"],
                   "oracle": "pass", "sha256": "hash", "native_code_sha256": "native"}
            row.update({k: 1 for k in ("compile_ns", "compile_peak_rss_bytes", "allocated_bytes",
                                      "first_result_ns", "first_result_peak_rss_bytes",
                                      "pipeline_allocated_bytes", "exec_ns")})
            return "STUDY_SAMPLE " + json.dumps(row)
        return "identity"

    def build_command(self, args, **kwargs):
        if args[:3] == ["git", "worktree", "add"]:
            base = Path(args[-2])
            (base / "bench/suite").mkdir(parents=True)
            (base / "src/wago").mkdir(parents=True)
        if args[:3] == ["go", "test", "-c"]:
            Path(args[4]).write_bytes(b"test binary")
        return subprocess.CompletedProcess(args, 0)

    def assert_failed_output(self):
        with self.assertRaises(type(self.failure)):
            cold.main()
        logs = list(self.output.rglob("raw.log"))
        self.assertEqual(len(logs), 1)
        self.assertIn("compile base fixture", logs[0].read_text())
        self.assertIn("failure diagnostic", logs[0].read_text())

    def test_nonzero_exit_preserves_diagnostics(self):
        self.failure = subprocess.CalledProcessError(2, "fixture", output="failure diagnostic\n")
        self.assert_failed_output()

    def test_timeout_preserves_byte_diagnostics(self):
        self.failure = subprocess.TimeoutExpired("fixture", 240, output=b"failure diagnostic\n")
        self.assert_failed_output()

    def test_failed_rerun_keeps_evidence_in_separate_directory(self):
        cold.main()
        summaries = list(self.output.rglob("summary.json"))
        self.assertEqual(len(summaries), 1)
        previous = summaries[0].read_bytes()
        self.failure = subprocess.CalledProcessError(2, "fixture", output="failure diagnostic\n")
        with self.assertRaises(subprocess.CalledProcessError):
            cold.main()
        logs = list(self.output.rglob("raw.log"))
        self.assertEqual(len(logs), 2)
        failed = next(p for p in logs if "failure diagnostic" in p.read_text())
        self.assertNotEqual(failed.parent, summaries[0].parent)
        self.assertFalse((failed.parent / "summary.json").exists())
        self.assertEqual(summaries[0].read_bytes(), previous)

    def test_dirty_source_is_rejected_before_building(self):
        self.dirty = " M src/wago/api.go\n?? extra.go"
        with self.assertRaisesRegex(SystemExit, "clean"):
            cold.main()
        self.commands.assert_not_called()


if __name__ == "__main__":
    unittest.main()
