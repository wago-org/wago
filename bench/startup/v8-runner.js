if (arguments.length !== 1) {
  throw new Error("usage: v8 v8-runner.js -- <module.wasm>");
}

const bytes = readbuffer(arguments[0]);
const mod = new WebAssembly.Module(bytes);
const instance = new WebAssembly.Instance(mod, {});

if (typeof instance.exports._start !== "function") {
  throw new Error("module does not export _start");
}

instance.exports._start();
