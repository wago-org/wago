// binary-trees: the canonical allocation/GC-heavy benchmark (Benchmarks-Game).
//
// Every node is a managed object with two managed reference fields, so this
// stresses exactly the two costs an external GC targets:
//   * allocation throughput (millions of short-lived `new TreeNode()`), and
//   * the write barrier (`node.left = ...` / `node.right = ...` are reference
//     stores that AS's `incremental` runtime routes through __link).
// A long-lived tree plus a taller "stretch" tree are retained across the loop
// so a tracing collector has real live-heap work, not just dead garbage.
//
// One source, three runtimes: build with `--runtime incremental|minimal|stub`.
// The explicit __collect() at each outer step is a full collection under
// incremental/minimal and a no-op under stub, so the collection *points* are
// identical across variants — the only difference under `incremental` is the
// per-store write barrier and the interleaved mark-stepping on each `__new`.
//
// wago has no start section: built with --exportStart _initialize (host calls
// it once after instantiate) and --disable simd for the core-1.0 backend.

class TreeNode {
  left: TreeNode | null = null;
  right: TreeNode | null = null;
}

function bottomUpTree(depth: i32): TreeNode {
  const node = new TreeNode();
  if (depth > 0) {
    node.left = bottomUpTree(depth - 1);
    node.right = bottomUpTree(depth - 1);
  }
  return node;
}

function itemCheck(node: TreeNode): i32 {
  const l = node.left;
  if (l === null) return 1;
  return 1 + itemCheck(l) + itemCheck(node.right!);
}

// run(maxDepth) -> checksum. Returns an i32 so the work isn't dead-code
// eliminated. maxDepth ~14-18 is a meaningful managed-heap workload.
export function run(maxDepth: i32): i32 {
  const minDepth = 4;
  if (maxDepth < minDepth + 2) maxDepth = minDepth + 2;

  const stretchDepth = maxDepth + 1;
  let total = itemCheck(bottomUpTree(stretchDepth));

  // Long-lived tree retained for the whole run (gives the tracer live heap).
  const longLived = bottomUpTree(maxDepth);

  for (let depth = minDepth; depth <= maxDepth; depth += 2) {
    const iterations = 1 << (maxDepth - depth + minDepth);
    let sum = 0;
    for (let i = 0; i < iterations; i++) {
      sum += itemCheck(bottomUpTree(depth));
    }
    total += sum;
    __collect(); // full collection point; no-op under --runtime stub
  }

  total += itemCheck(longLived);
  return total;
}
