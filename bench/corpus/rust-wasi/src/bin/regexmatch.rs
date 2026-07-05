// regex crate: compile a small alternation pattern and count matches over a
// generated corpus. Exercises the DFA's dense jump-table dispatch (br_table),
// which regressed under the backend's register allocator — see the
// br_table-index-in-RAX regression (railshot brtable_regalloc_test.go).
use regex::Regex;
fn main() {
    let re = Regex::new(r"(\w+)@(\w+)\.(com|org|net)").unwrap();
    let mut text = String::new();
    for i in 0..3000 {
        text.push_str(&format!("user{i}@host{}.org and noise {i} ", i % 7));
    }
    let n = re.find_iter(&text).count();
    println!("regex:{}:{}", n, text.len());
}
