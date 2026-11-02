#!/usr/bin/env python3
"""Run the existing bounded local validator without rewriting its receipt."""
import importlib.util
import sys
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
TARGET = ROOT / "docs/rfwgc-evidence/final-darwin/validate_local.py"
OUTPUT = Path(__file__).with_name("local-validation.json")
sys.path.insert(0, str(TARGET.parent))

spec = importlib.util.spec_from_file_location("pin_recheck_validate_local", TARGET)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(module)

original = Path.write_text


def redirected(self, data, *args, **kwargs):
    expected = module.contracts.REV / "local-validation.json"
    if self == expected:
        return original(OUTPUT, data, *args, **kwargs)
    return original(self, data, *args, **kwargs)


with patch.object(Path, "write_text", redirected):
    module.main()
