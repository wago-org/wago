"""Execute emitted store-map arithmetic and interleaved stores, independently."""
import base64
import json
import struct
import sys
from unicorn import Uc, UC_ARCH_ARM64, UC_MODE_ARM
from unicorn.arm64_const import UC_ARM64_REG_X0, UC_ARM64_REG_X1

cases = json.load(open(sys.argv[1]))
for index, case in enumerate(cases):
    machine = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
    machine.mem_map(0x10000, 0x10000)
    machine.mem_map(0x30000, 0x10000)
    code = base64.b64decode(case["Code"])
    machine.mem_write(0x10000, code)
    machine.mem_write(0x30000, struct.pack("<16I", *case["Input"]))
    machine.reg_write(UC_ARM64_REG_X0, 0x30000)
    machine.reg_write(UC_ARM64_REG_X1, 0x30100)
    machine.emu_start(0x10000, 0x10000 + len(code))
    got = list(struct.unpack("<8I", machine.mem_read(0x30040, 32)))
    assert got == case["Want"], (index, got, case["Want"])
    paired = list(struct.unpack("<8I", machine.mem_read(0x30100, 32)))
    want = [value for lane in range(4) for value in
            (case["Want"][lane], case["Want"][4 + lane])]
    assert paired == want, (index, paired, want)
print(f"PASS: {len(cases)} ARM64 vector arithmetic and interleaved-store cases")
