import assert from "node:assert/strict";
import test from "node:test";

import {
  SCHEMA,
  canonicalEvents,
  hex32,
  hashCanonical,
  mixInput64,
} from "./engine-state-oracle.mjs";

test("canonical engine-state bytes and hash match the Go golden", () => {
  const events = [
    ["schema", SCHEMA],
    ["input_i32", "00000002", "9f1512ab"],
    ["mark", "000006a4"],
    ["observe_i64", "00000047", "0123456789abcdef"],
    ["outcome", "returned"],
    ["memory", 0, ["__fuzz_memory_0"], 4, "sha256:9f64a747e1b97f131fabb6b447296c9b6f0201e79fb3c5356e6c77e89b6a806a"],
  ];
  const canonical = canonicalEvents(events);
  assert.equal(canonical, '[["schema","starshine.engine-state-events.v1"],["input_i32","00000002","9f1512ab"],["mark","000006a4"],["observe_i64","00000047","0123456789abcdef"],["outcome","returned"],["memory",0,["__fuzz_memory_0"],4,"sha256:9f64a747e1b97f131fabb6b447296c9b6f0201e79fb3c5356e6c77e89b6a806a"]]');
  assert.equal(hashCanonical(canonical), "sha256:68aa122e9c5a54daa31fe8b12b9afa91a6a236f353d492a781ed3f82cf98e2f8");
});

test("input mixer matches the Go golden", () => {
  assert.equal(mixInput64(0x5eedn, 2, 0x693332n), 0x6f18f9116c668c0fn);
  assert.equal(hex32(0x12345678937b2f66n), "937b2f66");
});
