#!/usr/bin/env python3
"""Exercise the real runners' aggregate status without performing any builds."""
import contextlib
import io
import json
from pathlib import Path
import runpy
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch


class GateRunnerStatus(unittest.TestCase):
    def exercise(self, script, manifest, count, failure=None, code=1):
        with tempfile.TemporaryDirectory(prefix='wago-runner-contract-') as directory:
            target = Path(directory) / 'repo' / 'evidence' / 'candidate' / script
            target.parent.mkdir(parents=True)
            shutil.copyfile(Path(__file__).with_name(script), target)
            invocations = []

            def execute(command, **kwargs):
                index = len(invocations)
                invocations.append(command)
                kwargs['stdout'].write('simulated gate output\n')
                return subprocess.CompletedProcess(command, code if index == failure else 0)

            with patch('subprocess.run', side_effect=execute), \
                    patch('subprocess.check_output', return_value='test-head\n'), \
                    patch('sys.argv', [str(target)]), \
                    contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaises(SystemExit) as exit_status:
                    runpy.run_path(str(target), run_name='__main__')
            self.assertEqual(exit_status.exception.code, 0 if failure is None else 1)
            records = json.loads(target.with_name(manifest).read_text())
            self.assertEqual(len(records), count)
            self.assertEqual(len(invocations), count)
            self.assertEqual([record['exit_code'] for record in records],
                             [code if index == failure else 0 for index in range(count)])

    def test_all_gates_pass(self):
        for script, manifest in [('run-gates.py', 'gates.json'), ('cross-compile.py', 'cross.json')]:
            with self.subTest(script=script):
                self.exercise(script, manifest, 12)

    def test_each_failed_or_signalled_gate_fails_runner(self):
        for script, manifest in [('run-gates.py', 'gates.json'), ('cross-compile.py', 'cross.json')]:
            for index in range(12):
                for code in [1, -11]:
                    with self.subTest(script=script, index=index, code=code):
                        self.exercise(script, manifest, 12, index, code)


if __name__ == '__main__':
    unittest.main()
