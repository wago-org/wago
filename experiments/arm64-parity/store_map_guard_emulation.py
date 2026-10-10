"""Compare emitted ARM64 range guards with the independently tested predicate."""
import base64
import json
import struct
import sys
from unicorn import Uc, UC_ARCH_ARM64, UC_MODE_ARM
from unicorn.arm64_const import (UC_ARM64_REG_X0, UC_ARM64_REG_X1,
    UC_ARM64_REG_X2, UC_ARM64_REG_X3, UC_ARM64_REG_X7)

cases = json.load(open(sys.argv[1]))
for index, case in enumerate(cases):
    machine = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
    for address in (0x10000, 0x30000, 0x50000):
        machine.mem_map(address, 0x10000)
    code = base64.b64decode(case["Code"])
    initial = struct.pack("<4Q", *case["Initial"])
    machine.mem_write(0x10000, code)
    machine.mem_write(0x30000, initial)
    machine.reg_write(UC_ARM64_REG_X0, 0x30000)
    machine.reg_write(UC_ARM64_REG_X1, 0x50000)
    machine.reg_write(UC_ARM64_REG_X2, case["Memory"])
    machine.emu_start(0x10000, 0x10000 + len(code), count=10000)
    safe = machine.reg_read(UC_ARM64_REG_X7) == 1
    assert safe == case["Safe"], (index, case["Initial"], case["Memory"], safe, case["Safe"])
    if safe:
        assert machine.reg_read(UC_ARM64_REG_X3) == case["Trips"], index
    assert bytes(machine.mem_read(0x30000, len(initial))) == initial, index
print(f"PASS: {len(cases)} ARM64 trip, wrap, bounds and alias guard cases")
