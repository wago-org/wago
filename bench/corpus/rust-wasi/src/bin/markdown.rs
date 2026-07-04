// pulldown-cmark: render a generated CommonMark document to HTML.
use pulldown_cmark::{html, Parser};
fn main() {
    let mut md = String::new();
    for i in 0..800 {
        md.push_str(&format!(
            "# Heading {i}\n\nSome **bold** and *italic* text with `code` and a [link](http://x{i}.org).\n\n- item {i}\n- item {}\n\n",
            i + 1
        ));
    }
    let mut out = String::new();
    html::push_html(&mut out, Parser::new(&md));
    println!("markdown: {} bytes md -> {} bytes html", md.len(), out.len());
}
