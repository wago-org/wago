// Persistent d8 worker for like-for-like module instantiation and exported-call
// timing. Invoke as: v8 v8-execution-worker.js -- <flags>.
const options = parseArgs(arguments);
const bytes = readbuffer(required("module"));
const module = new WebAssembly.Module(bytes);
const imports = defaultImports(module);
const targetNs = positiveInt(options.get("benchtime-ns") || "100000000");
const round = Number(options.get("round") || 0);

if (options.has("measure-instantiate")) {
  const instantiate = (n) => {
    const started = performance.now();
    for (let i = 0; i < n; i++) new WebAssembly.Instance(module, imports);
    return (performance.now() - started) * 1e6;
  };
  instantiate(1);
  const iterations = calibrate(instantiate, targetNs, 1 << 30);
  const elapsed = instantiate(iterations);
  append({ engine: "v8", stage: "instantiate", module: required("module"), round,
    iterations, elapsed_ns: Math.round(elapsed), ns_per_op: elapsed / iterations });
}

const instance = new WebAssembly.Instance(module, imports);
const init = options.get("init");
if (init) instance.exports[init]();
const exportName = required("export");
const fn = instance.exports[exportName];
if (typeof fn !== "function") throw new Error(`${exportName} is not a function`);
const callArgs = (options.get("args") || "").split(",").filter(Boolean).map(Number);
const invoke = (n) => {
  const started = performance.now();
  let result;
  for (let i = 0; i < n; i++) result = fn(...callArgs);
  globalThis.__wagoResult = result;
  return (performance.now() - started) * 1e6;
};
invoke(1);
const iterations = calibrate(invoke, targetNs, 2 ** 40);
const elapsed = invoke(iterations);
append({ engine: "v8", stage: "exec", module: required("module"), export: exportName,
  round, iterations, elapsed_ns: Math.round(elapsed), ns_per_op: elapsed / iterations });

function parseArgs(args) {
  const out = new Map();
  for (let i = 0; i < args.length; i++) {
    const arg = String(args[i]);
    if (!arg.startsWith("-") || (i + 1 >= args.length && arg !== "-measure-instantiate")) {
      throw new Error(`invalid argument ${arg}`);
    }
    const key = arg.replace(/^-+/, "");
    if (key === "measure-instantiate") out.set(key, "1");
    else out.set(key, String(args[++i]));
  }
  return out;
}
function required(name) {
  const value = options.get(name);
  if (!value) throw new Error(`-${name} is required`);
  return value;
}
function positiveInt(value) {
  const number = Number(value);
  if (!Number.isSafeInteger(number) || number <= 0) throw new Error("invalid benchtime");
  return number;
}
function calibrate(run, target, limit) {
  let iterations = 1;
  for (;;) {
    const elapsed = run(iterations);
    if (elapsed >= target / 10 || iterations >= limit) {
      return Math.max(1, Math.floor(iterations * target / Math.max(1, elapsed)));
    }
    iterations *= 10;
  }
}
function defaultImports(compiled) {
  const imports = {};
  for (const item of WebAssembly.Module.imports(compiled)) {
    imports[item.module] ||= {};
    if (item.kind !== "function") throw new Error(`unsupported ${item.kind} import ${item.module}.${item.name}`);
    imports[item.module][item.name] = () => 0;
  }
  return imports;
}
function append(row) {
  const path = required("out");
  let previous = "";
  try { previous = read(path) || ""; } catch (_) {}
  writeFile(path, previous + JSON.stringify(row) + "\n");
}
