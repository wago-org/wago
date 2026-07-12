import { UTF8 } from "./index";

// The unsafe length kernel takes the SIMD path without allocating an
// intermediate AssemblyScript string on every host invocation.
const text = "SIMD UTF-8 benchmark: Καλημέρα 世界 🚀 ".repeat(96);
const input = UTF8.encode(text);

export function convertN(n: i32): i32 {
  let sum = 0;
  const ptr = changetype<usize>(input);
  for (let i = 0; i < n; i++) sum += UTF8.utf16LengthUnsafe(ptr, input.byteLength);
  return sum;
}
