(module
  ;; Boundary-only host -> Wasm fixture. Keeping the body to one identity
  ;; operation prevents guest computation from contaminating call overhead.
  (func (export "call") (param i32) (result i32)
    local.get 0))
