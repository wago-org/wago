import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { performance } from "node:perf_hooks";
import { fileURLToPath } from "node:url";
import { runInNewContext } from "node:vm";
import test from "node:test";

const toolsDir = dirname(fileURLToPath(import.meta.url));
const benchDir = dirname(toolsDir);

test("coerces i64 export arguments to BigInt without mutating the timed instance", () => {
  const script = readFileSync(join(toolsDir, "v8-execution-worker.js"), "utf8");
  const modulePath = join(benchDir, "corpus", "xjb-mulhi.wasm");
  let output = "";
  const context = {
    arguments: [
      "-module", modulePath,
      "-export", "mulhi",
      "-args", "305419896,-1698898192",
      "-round", "0",
      "-benchtime-ns", "1000",
      "-out", "result.jsonl",
    ],
    performance,
    WebAssembly,
    readbuffer(path) {
      const buffer = readFileSync(path);
      return buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength);
    },
    read() { throw new Error("missing output"); },
    writeFile(_path, value) { output = value; },
  };

  runInNewContext(script, context);
  const result = JSON.parse(output.trim());
  assert.equal(result.engine, "v8");
  assert.equal(result.export, "mulhi");
  assert.ok(result.iterations > 0);
});
