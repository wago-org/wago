// Independent Node/V8 oracle; run from this directory with Node 22+.
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
const wasm = readFileSync(new URL('pdfium-8111.wasm', import.meta.url));
const pdf = readFileSync(new URL('invoice.pdf', import.meta.url));
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const module = new WebAssembly.Module(wasm);
const imports = {};
let e;
for (const { module: namespace, name, kind } of WebAssembly.Module.imports(module)) {
  if (kind !== 'function') throw new Error(`Unexpected import ${namespace}.${name}`);
  (imports[namespace] ??= {})[name] = (...args) => {
    if (namespace === 'env' && name === 'emscripten_notify_memory_growth' && args[0] === 0) return;
    if (namespace === 'wasi_snapshot_preview1' && name === 'fd_prestat_get' && args[0] === 3) return 8; // EBADF: no preopens.
    if (namespace === 'wasi_snapshot_preview1' && name === 'environ_sizes_get') {
      const memory = new DataView(e.memory.buffer);
      memory.setUint32(args[0], 0, true);
      memory.setUint32(args[1], 0, true);
      return 0;
    }
    if (namespace === 'wasi_snapshot_preview1' && name === 'environ_get') {
      new DataView(e.memory.buffer).setUint32(args[0], 0, true);
      return 0;
    }
    throw new Error(`Unexpected host call ${namespace}.${name}(${args})`);
  };
}
e = new WebAssembly.Instance(module, imports).exports;
e._initialize();
e.FPDF_InitLibrary();
const ptr = e.malloc(pdf.length);
new Uint8Array(e.memory.buffer, ptr, pdf.length).set(pdf);
const doc = e.FPDF_LoadMemDocument(ptr, pdf.length, 0);
if (!doc) throw new Error(`Load error ${e.FPDF_GetLastError()}`);
const result = { wasm_sha256: hash(wasm), input_sha256: hash(pdf), pages: [] };
for (let index = 0; index < e.FPDF_GetPageCount(doc); index++) {
  const page = e.FPDF_LoadPage(doc, index);
  const textPage = e.FPDFText_LoadPage(page);
  const chars = e.FPDFText_CountChars(textPage);
  const buf = e.malloc((chars + 1) * 2);
  const units = e.FPDFText_GetText(textPage, 0, chars, buf);
  const text = Buffer.from(e.memory.buffer, buf, (units - 1) * 2).toString('utf16le');
  const bitmap = e.FPDFBitmap_Create(96, 128, 1);
  e.FPDFBitmap_FillRect(bitmap, 0, 0, 96, 128, 0xffffffff);
  e.FPDF_RenderPageBitmap(bitmap, page, 0, 0, 96, 128, 0, 0);
  const pixels = Buffer.from(e.memory.buffer, e.FPDFBitmap_GetBuffer(bitmap), e.FPDFBitmap_GetStride(bitmap) * 128);
  result.pages.push({ width: e.FPDF_GetPageWidth(page), height: e.FPDF_GetPageHeight(page), text, bitmap_sha256: hash(pixels) });
  e.FPDFBitmap_Destroy(bitmap);
  e.free(buf);
  e.FPDFText_ClosePage(textPage);
  e.FPDF_ClosePage(page);
}
e.FPDF_CloseDocument(doc);
e.free(ptr);
e.FPDF_DestroyLibrary();
console.log(JSON.stringify(result, null, 2));
