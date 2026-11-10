// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import {
  addNotice,
  isExcludedSource,
  processFiles,
  styleFor,
} from "./license-headers.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("notices preserve executable preambles, build tags and original source", () => {
  for (const [source, style, prefix] of [
    ["//go:build arm64\n\npackage sample\n", "//", ""],
    ["#!/bin/bash\nset -e\n", "#", "#!/bin/bash\n"],
    [
      "#!/usr/bin/env python3\n# coding: utf-8\nprint(1)\n",
      "#",
      "#!/usr/bin/env python3\n# coding: utf-8\n",
    ],
    [
      "# Python program\n# coding: latin-1\nprint(1)\n",
      "#",
      "# Python program\n# coding: latin-1\n",
    ],
    ["\uFEFF#!/bin/sh\r\nexit 0\r\n", "#", "\uFEFF#!/bin/sh\r\n"],
    ["#![no_std]\nfn main() {}\n", "//", ""],
    ["(module)\n", ";;", ""],
    ["// Copyright Original Author\npackage sample\n", "//", ""],
    ['const text = "SPDX-License-Identifier: MPL-2.0";\n', "//", ""],
    ["", "//", ""],
  ]) {
    const output = addNotice(source, style);
    assert(output.startsWith(prefix));
    assert(output.endsWith(source.slice(prefix.length)));
    assert.equal(addNotice(output, style), output);
    assert(output.includes("SPDX-License-Identifier: MPL-2.0"));
    if (source.includes("\r\n"))
      assert(!output.replaceAll("\r\n", "").includes("\n"));
  }
  assert.throws(
    () => addNotice("// SPDX-License-Identifier: MIT\n", "//"),
    /Refusing/,
  );
  assert.throws(
    () => addNotice("package sample\n" + addNotice("", "//"), "//"),
    /misplaced/,
  );
});

test("headers cover owned code, excluding configuration and corpus builders", () => {
  for (const file of [
    "file.go",
    "src/core/runtime/entry_arm64.s",
    "src/core/runtime/inline_host_yield_arm64.h",
    "scripts/license-headers.mjs",
    "examples/01-hello/main.go",
  ])
    assert.equal(styleFor(file), "//");
  for (const file of [
    "install.ps1",
    ".githooks/pre-commit",
    "scripts/run.py",
    "src/core/encoder/riscv64/testdata/float.s",
  ])
    assert.equal(styleFor(file), "#");
  for (const file of [
    "go.mod",
    "schema.json",
    "README.md",
    "module.wasm",
    "LICENSE",
    ".just/lint.just",
    "justfile",
    ".github/workflows/ci.yml",
    ".github/dependabot.yml",
    "unknown/borrowed.go",
    "vendor/dependency/file.go",
    "corpus/build/rust.sh",
    "corpus/sources/rust/kernel.rs",
    "corpus/workloads/semantic/coremark/core_portme.c",
    "tests/corpus/regressions/pdfium/oracle.mjs",
    "tests/fixtures/wasm/example.wat",
    "src/wago/testdata/example.wat",
    "bench/startup/src/nbody.rs",
    "examples/22-language-guests/tinygo/main.go",
    "examples/22-language-guests/build.sh",
    "file.rs",
  ])
    assert.equal(styleFor(file), null, file);
  assert(isExcludedSource("corpus/build/rust.sh"));
  assert(isExcludedSource("tests/corpus/regressions/pdfium/oracle.mjs"));
  assert(!isExcludedSource("scripts/license-headers.mjs"));
});

test("repository check fails missing notices, repairs new sources, and preserves fixtures", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "wago-license-"));
  try {
    fs.mkdirSync(path.join(dir, "scripts"));
    fs.copyFileSync(
      path.join(root, "scripts/license-headers.mjs"),
      path.join(dir, "scripts/license-headers.mjs"),
    );
    execFileSync("git", ["init", "-q"], { cwd: dir });
    fs.writeFileSync(
      path.join(dir, "example.go"),
      "//go:build arm64\n\npackage sample\n",
    );
    execFileSync("git", ["add", "example.go"], { cwd: dir });
    fs.writeFileSync(path.join(dir, "untracked.sh"), "#!/bin/sh\nexit 0\n");
    const fixture = "corpus/repro/tool/inputs/main.c";
    fs.mkdirSync(path.dirname(path.join(dir, fixture)), { recursive: true });
    fs.writeFileSync(
      path.join(dir, fixture),
      "// SPDX-License-Identifier: MIT\nint main() {}\n",
    );
    fs.writeFileSync(path.join(dir, "data.json"), '{"x":1}\n');
    const excluded = new Map([
      [".github/workflows/ci.yml", "name: CI\n"],
      [".just/lint.just", "all:\n    true\n"],
      ["justfile", "default:\n    true\n"],
      ["corpus/sources/rust/kernel.rs", "fn main() {}\n"],
      ["corpus/build/rust.sh", "#!/bin/sh\nexit 0\n"],
      ["bench/startup/src/nbody.rs", "fn main() {}\n"],
      ["examples/22-language-guests/tinygo/main.go", "package main\n"],
      [
        "unknown/borrowed.go",
        "// SPDX-License-Identifier: MIT\npackage borrowed\n",
      ],
    ]);
    for (const [file, content] of excluded) {
      fs.mkdirSync(path.dirname(path.join(dir, file)), { recursive: true });
      fs.writeFileSync(path.join(dir, file), content);
    }
    const run = (flag) =>
      spawnSync(process.execPath, ["scripts/license-headers.mjs", flag], {
        cwd: dir,
        encoding: "utf8",
      });
    assert.equal(run("--check").status, 1);
    assert.equal(run("--write").status, 0);
    assert.equal(run("--check").status, 0);
    assert.equal(
      fs.readFileSync(path.join(dir, fixture), "utf8"),
      "// SPDX-License-Identifier: MIT\nint main() {}\n",
    );
    assert.equal(
      fs.readFileSync(path.join(dir, "data.json"), "utf8"),
      '{"x":1}\n',
    );
    for (const [file, content] of excluded)
      assert.equal(
        fs.readFileSync(path.join(dir, file), "utf8"),
        content,
        file,
      );
    assert.equal(processFiles(dir, [fixture], true).preserved, 1);
    assert.equal(run("--invalid").status, 1);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
