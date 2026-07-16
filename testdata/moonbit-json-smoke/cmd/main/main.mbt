///|
let corpus : String =
  #|{"schema":"wago-json-smoke-v1","generation":7,"active":true,
  #|"users":[
  #|{"id":1,"name":"Ada Lovelace","roles":["admin","compiler"],"scores":[3,5,8,13],"profile":{"city":"London","unicode":"λ月🚀"}},
  #|{"id":2,"name":"Grace Hopper","roles":["navy","compiler"],"scores":[21,34,55],"profile":{"city":"Arlington","unicode":"COBOL"}},
  #|{"id":3,"name":"Edsger Dijkstra","roles":["researcher"],"scores":[],"profile":null}],
  #|"matrix":[[1,2,3],[5,8,13],[21,34,55]],
  #|"metrics":{"min":-17,"max":2147483647,"ratio":0.125,"exponent":6.022e23},
  #|"flags":[true,false,null,true],
  #|"escaped":"line one\\nline two\\t\\\"quoted\\\"\\\\slash",
  #|"volatile":{"timestamp":1720000000,"nonce":"remove-me"}}

///|
fn checksum(seed : Int64, text : String) -> Int64 {
  let view = text[:]
  let mut hash = seed
  for i = 0; i < view.length(); i = i + 1 {
    hash = (hash * 131L + view[i].to_int64()) % 2147483629L
  }
  hash
}

///|
fn run_checked(iterations : Int) -> Int64 raise {
  let count = if iterations < 1 { 1 } else { iterations }
  let mut result = 146959810L
  for i = 0; i < count; i = i + 1 {
    let parsed = @json.parse(corpus)
    let rendered = parsed.stringify(indent=0)
    let reparsed = @json.parse(rendered)
    if !(parsed == reparsed) {
      return -1L
    }
    result = checksum(result + i.to_int64(), rendered)
  }
  result
}

///|
/// Parse, stringify, reparse, compare, and checksum the embedded JSON corpus.
/// Parse errors return -2 and a round-trip mismatch returns -1.
pub fn run(iterations : Int) -> Int64 {
  try run_checked(iterations) catch {
    _ => -2L
  }
}

///|
fn main {
}
