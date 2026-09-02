import { createHash } from "node:crypto";

export const SCHEMA = "starshine.engine-state-events.v1";
export const I32_SALT = 0x693332n;
export const I64_SALT = 0x693634n;

const MASK64 = (1n << 64n) - 1n;
const MEMORY_LIMIT_BYTES = 2 * 65536;
const TABLE_ENTRY_LIMIT = 32;

function u64(value) {
  return value & MASK64;
}

export function mixInput64(seed, channel, salt) {
  let value = u64(seed ^ u64(BigInt(channel) * 0x9e3779b97f4a7c15n) ^ salt);
  value = u64((value ^ (value >> 30n)) * 0xbf58476d1ce4e5b9n);
  value = u64((value ^ (value >> 27n)) * 0x94d049bb133111ebn);
  return u64(value ^ (value >> 31n));
}

export function hex32(value) {
  const bits = typeof value === "bigint" ? Number(BigInt.asUintN(32, value)) : Number(value) >>> 0;
  return bits.toString(16).padStart(8, "0");
}

export function hex64(value) {
  return BigInt.asUintN(64, BigInt(value)).toString(16).padStart(16, "0");
}

export function canonicalEvents(events) {
  return JSON.stringify(events);
}

export function hashCanonical(canonical) {
  return `sha256:${createHash("sha256").update(canonical).digest("hex")}`;
}

function normalizeTrap(error, expectedFamily = "") {
  if (!(error instanceof WebAssembly.RuntimeError)) {
    throw error;
  }
  const message = error.message.toLowerCase();
  if (message.includes("unreachable")) return "explicit-unreachable";
  if (message.includes("divide by zero")) return "integer-divide-by-zero";
  if (message.includes("divide result unrepresentable") || message.includes("integer overflow")) {
    return "signed-integer-division-overflow";
  }
  if (message.includes("float unrepresentable") || message.includes("invalid conversion")) {
    return "invalid-conversion-to-integer";
  }
  if (message.includes("memory access out of bounds")) return "out-of-bounds-memory-access";
  if (message.includes("table index is out of bounds") || message.includes("table out of bounds")) {
    return "out-of-bounds-table-access";
  }
  if ((message.includes("null function") || message.includes("signature mismatch")) &&
      (expectedFamily === "null-function-reference" || expectedFamily === "indirect-call-type-mismatch")) {
    return expectedFamily;
  }
  if (message.includes("null function")) return "null-function-reference";
  if (message.includes("function signature mismatch") || message.includes("indirect call type mismatch")) {
    return "indirect-call-type-mismatch";
  }
  throw new Error(`unclassified Node WebAssembly trap: ${error.message}`);
}

function normalizeInstantiationFailure(error, expectedFamily) {
  const message = error?.message?.toLowerCase?.() || "";
  if (expectedFamily === "active-data-out-of-bounds") {
    if (!(error instanceof WebAssembly.RuntimeError) || !message.includes("out of bounds")) throw error;
    return expectedFamily;
  }
  if (expectedFamily === "memory-import-limit-mismatch") {
    if (!(error instanceof WebAssembly.LinkError) || !message.includes("memory")) throw error;
    return expectedFamily;
  }
  if (expectedFamily === "table-import-limit-mismatch") {
    if (!(error instanceof WebAssembly.LinkError) || !message.includes("table")) throw error;
    return expectedFamily;
  }
  throw new Error(`unclassified Node instantiation failure: ${error?.message || error}`);
}

function normalizeCompileFailure(error, expectedFamily) {
  if (!(error instanceof WebAssembly.CompileError)) throw error;
  const message = error.message.toLowerCase();
  if (expectedFamily === "invalid-magic" && !message.includes("magic")) throw error;
  if (expectedFamily === "invalid-version" && !message.includes("version")) throw error;
  if (expectedFamily === "truncated-binary" || expectedFamily === "malformed-section-length") {
    if (!message.includes("end") && !message.includes("section") && !message.includes("length")) throw error;
  } else if (expectedFamily !== "invalid-magic" && expectedFamily !== "invalid-version") {
    throw new Error(`unclassified Node compile failure: ${error.message}`);
  }
  return expectedFamily;
}

function resourceGroups(module) {
  const groups = new Map();
  for (const descriptor of WebAssembly.Module.exports(module)) {
    const match = /^__fuzz_(global|memory|table)(?:_alias)?_(\d+)$/.exec(descriptor.name);
    if (!match) continue;
    if (descriptor.kind !== match[1]) {
      throw new Error(`synthetic export ${descriptor.name} has kind ${descriptor.kind}`);
    }
    const key = `${match[1]}:${Number(match[2])}`;
    const group = groups.get(key) ?? { kind: match[1], index: Number(match[2]), names: [] };
    group.names.push(descriptor.name);
    groups.set(key, group);
  }
  return [...groups.values()]
    .map((group) => ({ ...group, names: group.names.sort() }))
    .sort((a, b) => {
      const kindOrder = { global: 0, memory: 1, table: 2 };
      return kindOrder[a.kind] - kindOrder[b.kind] || a.index - b.index;
    });
}

function stateImports(caseSeed) {
  return {
    global: new WebAssembly.Global({ value: "i32", mutable: true }, 0),
    constGlobal: new WebAssembly.Global({ value: "i32", mutable: false }, Number(caseSeed & 3n)),
    memory: new WebAssembly.Memory({ initial: 1, maximum: 2 }),
    table: new WebAssembly.Table({ element: "anyfunc", initial: 4, maximum: 8 }),
  };
}

function resourceValue(group, instance, imported) {
  if (instance) return instance.exports[group.names[0]];
  return imported[group.kind];
}

function appendState(events, module, instance, imported) {
  const groups = resourceGroups(module);
  const functionImports = WebAssembly.Module.imports(module).filter((item) => item.kind === "function").length;
  const functionIndexes = new Map();
  if (instance) {
    for (const descriptor of WebAssembly.Module.exports(module)) {
      const match = /^__fuzz_func_(\d+)$/.exec(descriptor.name);
      if (match && descriptor.kind === "function") {
        functionIndexes.set(instance.exports[descriptor.name], functionImports + Number(match[1]));
      }
    }
  }

  for (const group of groups) {
    const value = resourceValue(group, instance, imported);
    if (group.kind === "global") {
      events.push(["global", group.index, group.names, "i32", hex32(value.value)]);
      continue;
    }
    if (group.kind === "memory") {
      const bytes = new Uint8Array(value.buffer);
      if (bytes.byteLength > MEMORY_LIMIT_BYTES) {
        throw new Error(`memory ${group.index} has ${bytes.byteLength} bytes; limit is ${MEMORY_LIMIT_BYTES}`);
      }
      const digest = createHash("sha256").update(bytes).digest("hex");
      events.push(["memory", group.index, group.names, bytes.byteLength, `sha256:${digest}`]);
      continue;
    }
    const length = Number(value.length);
    if (length > TABLE_ENTRY_LIMIT) {
      throw new Error(`table ${group.index} has ${length} entries; limit is ${TABLE_ENTRY_LIMIT}`);
    }
    const relations = [];
    for (let entry = 0; entry < length; entry++) {
      let ref;
      try {
        ref = value.get(entry);
      } catch (error) {
        if (!(error instanceof TypeError) || !error.message.includes("BigInt")) throw error;
        ref = value.get(BigInt(entry));
      }
      if (ref === null) {
        relations.push("null");
      } else if (functionIndexes.has(ref)) {
        relations.push(`funcidx:${functionIndexes.get(ref)}`);
      } else if (!instance) {
        // A trapping start does not return its partial JS instance. The
        // imported table survives, so nullness is the portable relation.
        relations.push("non-null");
      } else {
        throw new Error(`table ${group.index} entry ${entry} has no synthetic function relation`);
      }
    }
    events.push(["table", group.index, group.names, length, relations]);
  }
}

// observeInNode instantiates one generated module. Its imports record the same
// schema as the Go plug-in, but this implementation shares no oracle code.
export function observeInNode(moduleBytes, caseSeed, options = {}) {
  const events = [["schema", SCHEMA]];
  let module;
  try {
    module = new WebAssembly.Module(moduleBytes);
  } catch (error) {
    if (options.outcomeKind !== "compile-failure") throw error;
    events.push(["outcome", "compile-failed", normalizeCompileFailure(error, options.trapFamily)]);
    const canonical = canonicalEvents(events);
    return { events, canonical, hash: hashCanonical(canonical) };
  }
  const supportBytes = options.supportModuleBytesList ??
    (options.supportModuleBytes?.byteLength ? [options.supportModuleBytes] : []);
  let supportInstance = null;
  for (const bytes of supportBytes) {
    const supportModule = new WebAssembly.Module(bytes);
    supportInstance = new WebAssembly.Instance(
      supportModule,
      supportInstance ? { __link: supportInstance.exports } : {},
    );
  }
  const imported = stateImports(caseSeed);
  const fuzz = {
    input_i32(channel) {
      const result = mixInput64(caseSeed, channel >>> 0, I32_SALT);
      events.push(["input_i32", hex32(channel), hex32(result)]);
      return Number(BigInt.asIntN(32, result));
    },
    input_i64(channel) {
      const result = mixInput64(caseSeed, channel >>> 0, I64_SALT);
      events.push(["input_i64", hex32(channel), hex64(result)]);
      return BigInt.asIntN(64, result);
    },
    mark(id) {
      events.push(["mark", hex32(id)]);
    },
    observe_i32(id, value) {
      events.push(["observe_i32", hex32(id), hex32(value)]);
    },
    observe_i64(id, value) {
      events.push(["observe_i64", hex32(id), hex64(value)]);
    },
    state_global_i32: imported.global,
    const_i32: imported.constGlobal,
    state_memory: imported.memory,
    state_table: imported.table,
  };

  let instance = null;
  try {
    instance = new WebAssembly.Instance(module, {
      __fuzz: fuzz,
      __link: supportInstance?.exports,
    });
    events.push(["outcome", "returned"]);
  } catch (error) {
    if (options.outcomeKind === "instantiation-failure") {
      events.push(["outcome", "instantiation-failed", normalizeInstantiationFailure(error, options.trapFamily)]);
    } else {
      events.push(["outcome", "trapped", normalizeTrap(error, options.trapFamily)]);
    }
  }
  if (options.outcomeKind !== "instantiation-failure") {
    appendState(events, module, instance, imported);
  }
  const canonical = canonicalEvents(events);
  return { events, canonical, hash: hashCanonical(canonical) };
}

export const intendedTrapClasses = Object.freeze({
  unreachable: "explicit-unreachable",
  "integer-division-by-zero": "integer-divide-by-zero",
  "signed-division-overflow": "signed-integer-division-overflow",
  "invalid-float-to-integer-conversion": "invalid-conversion-to-integer",
  "out-of-bounds-memory": "out-of-bounds-memory-access",
  "out-of-bounds-table": "out-of-bounds-table-access",
  "null-function-reference": "null-function-reference",
  "indirect-call-type-mismatch": "indirect-call-type-mismatch",
  "active-data-out-of-bounds": "active-data-out-of-bounds",
  "memory-import-limit-mismatch": "memory-import-limit-mismatch",
  "table-import-limit-mismatch": "table-import-limit-mismatch",
  "invalid-magic": "invalid-magic",
  "invalid-version": "invalid-version",
  "truncated-binary": "truncated-binary",
  "malformed-section-length": "malformed-section-length",
});
