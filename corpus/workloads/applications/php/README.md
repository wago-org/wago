# PHP 8.2.6 CLI

`php.wasm` is the WasmEdge-flavored PHP CLI asset from the
`vmware-labs/webassembly-language-runtimes` release
`php/8.2.6+20230714-11be424`; SHA-256
`5461eea8426378e2257f46c7eb1c734f607a89de9a921b72f8fc14c20ac3a33a`.
The PHP and WLR license notices are retained here.

The module imports five WasmEdge socket extensions absent from standard WASI
Preview 1. The corpus host binds them to `ENOSYS`; Wago's Preview 1 host
already provides the sixth, `sock_accept`, as well as the standard socket
calls. No network access is granted. The workload reads a PHP script on stdin
and emits the exact seventeen-bucket result independently checked with native
PHP and wazero. This does not claim socket functionality or a packaged PHP
extension environment.
