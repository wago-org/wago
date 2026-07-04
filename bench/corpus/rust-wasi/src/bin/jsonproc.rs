// serde_json: parse a generated array, aggregate, re-serialize.
use serde_json::Value;
fn main() {
    let mut s = String::from("[");
    for i in 0..2000u64 { if i > 0 { s.push(','); } s.push_str(&format!("{{\"id\":{i},\"v\":{}}}", (i * 2654435761) % 100000)); }
    s.push(']');
    let v: Value = serde_json::from_str(&s).unwrap();
    let sum: u64 = v.as_array().unwrap().iter().map(|e| e["v"].as_u64().unwrap()).sum();
    println!("json:{}:{}", v.as_array().unwrap().len(), sum);
}
