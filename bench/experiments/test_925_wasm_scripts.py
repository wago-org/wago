"""Regression checks for WABT's terminal empty-function formatting."""
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


@unittest.skipUnless(shutil.which("wat2wasm") and shutil.which("wasm2wat"), "requires WABT")
class WasmScriptTests(unittest.TestCase):
    def test_function_bodies(self):
        scripts = Path(__file__).resolve().parent
        for source, functions in [
            ("(module (func))", [0]),
            ("(module (func) (func))", [0, 1]),
            ('(module (func (export "run")))', [0]),
            ("(module (func nop))", [0]),
            ('(module (import "env" "a" (func)) (import "env" "b" (func)) (func))', [2]),
            ('(module (@custom "test" "payload") (func))', [0]),
        ]:
            with self.subTest(source=source), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                wat, wasm = root / "input.wat", root / "input.wasm"
                wat.write_text(source)
                subprocess.run(["wat2wasm", "--enable-annotations", str(wat), "-o", str(wasm)], check=True)
                profiled = root / "profiled.wasm"
                subprocess.run([sys.executable, str(scripts / "925-profile-functions.py"), str(wasm), str(profiled)], check=True)
                text = subprocess.check_output(["wasm2wat", str(profiled)], text=True)
                self.assertEqual(text.count("global.set "), len(functions))
                self.assertEqual(json.loads(profiled.with_suffix(".functions.json").read_text()), functions)
                entered = root / "entered.json"
                for visited in [[], functions]:
                    entered.write_text(json.dumps(visited))
                    ceiling = root / "ceiling.wasm"
                    subprocess.run([sys.executable, str(scripts / "925-unentered-body-ceiling.py"), str(wasm), str(entered), str(ceiling)], check=True, capture_output=True)
                    text = subprocess.check_output(["wasm2wat", str(ceiling)], text=True)
                    self.assertEqual(text.count("unreachable"), len(functions) - len(visited))


if __name__ == "__main__":
    unittest.main()
