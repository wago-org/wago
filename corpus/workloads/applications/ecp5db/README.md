# ECP5 device database subset

This is the LFE5U-25F device subset from the YoWASP ECP5 `0.11.1.0.post826`
wheel: `devices.json`, the LFE5U-25F device files, and ECP5 tile data needed
by `ecppack` and `ecpunpack`. The source Project Trellis database is CC0 1.0;
its license text is retained as `COPYING` outside the hashed `db/` mount.
The source wheel SHA-256 is
`42c70c022cc2e0620761db725b5b57aa5c6b9b7e32b1f31b29343fc10a767986`.

The corpus validates every relative filename and file digest using a sorted
tree digest: for each file, append `relative/path`, a NUL byte, lowercase hex
SHA-256 of its contents, and newline; SHA-256 the concatenation. The digest
is `688d3f0b7f408f18bfd98423fdee9403ae8366efd8618ae09fc681b3ea93f280`.
