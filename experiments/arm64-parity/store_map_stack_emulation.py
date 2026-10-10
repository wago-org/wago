"""Check SP-derived workspace addresses, including large frame displacements."""
import base64
import json
import sys
from unicorn import Uc, UC_ARCH_ARM64, UC_MODE_ARM
from unicorn.arm64_const import UC_ARM64_REG_SP, UC_ARM64_REG_X9

cases = json.load(open(sys.argv[1]))
for case in cases:
    machine = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
    machine.mem_map(0x10000, 0x10000)
    code = base64.b64decode(case["Code"])
    machine.mem_write(0x10000, code)
    machine.reg_write(UC_ARM64_REG_SP, 0x30000)
    machine.emu_start(0x10000, 0x10000 + len(code))
    assert machine.reg_read(UC_ARM64_REG_X9) == 0x30000 + case["Offset"], case
print(f"PASS: {len(cases)} ARM64 stack workspace address cases")
