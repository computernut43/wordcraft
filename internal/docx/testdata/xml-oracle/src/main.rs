//! Reads inputs from stdin, each a little-endian u32 length and that many bytes, and prints one
//! JSON line per input: the tree `xml::parse` builds, or `{"error": true}`. The tree is
//! `{"name", "attrs": [[k, v]…], "kids": [element or text…]}`.

#[allow(dead_code)]
#[path = "../../../../../crates/docx/src/xml.rs"]
mod xml;

use std::io::{Read, Write};

/// The variants of the crate's error that xml.rs uses.
#[allow(dead_code)]
#[derive(Debug)]
pub enum DocxError {
    Xml(String),
    Limit(String),
}

fn tree(e: &xml::El) -> serde_json::Value {
    let kids: Vec<serde_json::Value> = e
        .kids
        .iter()
        .map(|n| match n {
            xml::Node::El(c) => tree(c),
            xml::Node::Text(t) => serde_json::Value::String(t.clone()),
        })
        .collect();
    serde_json::json!({"name": e.name, "attrs": e.attrs, "kids": kids})
}

fn main() {
    let mut input = Vec::new();
    std::io::stdin().read_to_end(&mut input).unwrap();
    let mut out = std::io::BufWriter::new(std::io::stdout().lock());
    let mut rest = &input[..];
    while rest.len() >= 4 {
        let n = u32::from_le_bytes([rest[0], rest[1], rest[2], rest[3]]) as usize;
        let item = &rest[4..4 + n];
        rest = &rest[4 + n..];
        let v = match xml::parse(item) {
            Ok(root) => tree(&root),
            Err(_) => serde_json::json!({"error": true}),
        };
        writeln!(out, "{v}").unwrap();
    }
}
