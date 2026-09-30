#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

let state = 0x243f6a88;
function next() {
  state ^= state << 13;
  state ^= state >>> 17;
  state ^= state << 5;
  return state >>> 0;
}

const lines = [
  "const registry = new Map();",
  "const metrics = [];",
];

for (let index = 0; index < 6000; index++) {
  const suffix = next().toString(36).padStart(7, "0");
  const a = next() % 65521;
  const b = next() % 65521;
  const c = next() % 65521;
  lines.push(
    `class Processor_${suffix}_${index} {`,
    `  constructor(scale = ${a}) { this.scale = scale; this.offset = ${b}; }`,
    `  transform(value) { return (Math.imul(value ^ ${c}, this.scale) + this.offset) >>> 0; }`,
    `  summarize(values) { return values.reduce((sum, value) => sum + this.transform(value), 0); }`,
    `}`,
    `registry.set("processor-${suffix}-${index}", new Processor_${suffix}_${index}());`,
  );
}

lines.push(
  "for (const [name, processor] of registry) {",
  "  const values = Array.from({ length: 32 }, (_, index) => index * 17 + name.length);",
  "  metrics.push({ name, value: processor.summarize(values) });",
  "}",
  "metrics.sort((left, right) => right.value - left.value || left.name.localeCompare(right.name));",
  "console.log(JSON.stringify({ count: metrics.length, top: metrics.slice(0, 10) }));",
  "",
);

const generated = lines.join("\n");
const committed = fileURLToPath(new URL("inputs/source.js", import.meta.url));
const output = process.argv[2];
if (output) {
  writeFileSync(output, generated);
} else if (readFileSync(committed, "utf8") !== generated) {
  throw new Error("inputs/source.js is stale; run: node generate.mjs inputs/source.js");
}
