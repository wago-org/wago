#!/usr/bin/env node

import fs from "node:fs";
import vm from "node:vm";

const [gluePath, wasmPath, stdinPath, ...args] = process.argv.slice(2);
if (!gluePath || !wasmPath || !stdinPath) {
  console.error("usage: emscripten-v8-oracle.mjs GLUE WASM STDIN [ARGS...]");
  process.exit(2);
}

const input = fs.readFileSync(stdinPath, "utf8");
const lines = input.endsWith("\n") ? input.slice(0, -1).split("\n") : input.split("\n");
let line = 0;
const stdout = [];
const stderr = [];
const moduleConfig = {
  arguments: args,
  wasmBinary: fs.readFileSync(wasmPath),
  print: (value) => stdout.push(String(value)),
  printErr: (value) => stderr.push(String(value)),
};
const sandbox = {
  WebAssembly,
  TextDecoder,
  TextEncoder,
  console,
  crypto: globalThis.crypto,
  readline: () => line < lines.length ? lines[line++] : null,
  setInterval,
  clearInterval,
  setTimeout,
};
sandbox.globalThis = sandbox;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(gluePath, "utf8"), sandbox, { filename: gluePath });
const factory = vm.runInContext("Module", sandbox);
const loaded = await factory(moduleConfig);
loaded.callMain(args);
if (stdout.length) process.stdout.write(`${stdout.join("\n")}\n`);
if (stderr.length) process.stderr.write(`${stderr.join("\n")}\n`);
