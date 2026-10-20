//! Reads inputs from stdin, each a little-endian u32 length and that many bytes of a document's
//! JSON, and prints one line per input: the .docx `wordcraft_docx::write` makes of it, in hex,
//! or `ERR <message>`, or PANIC.
use std::io::{Read, Write};

fn answer(json: &[u8]) -> String {
    let doc: wordcraft_doc::Document = match serde_json::from_slice(json) {
        Ok(d) => d,
        Err(e) => return format!("ERR json {e}"),
    };
    match wordcraft_docx::write(&doc) {
        Ok(b) => b.iter().map(|x| format!("{x:02x}")).collect(),
        Err(e) => format!("ERR {e}"),
    }
}

fn main() {
    std::panic::set_hook(Box::new(|_| {}));
    let mut input = Vec::new();
    if std::io::stdin().read_to_end(&mut input).is_err() {
        return;
    }
    let mut out = std::io::BufWriter::new(std::io::stdout().lock());
    let mut rest = &input[..];
    while rest.len() >= 4 {
        let n = u32::from_le_bytes([rest[0], rest[1], rest[2], rest[3]]) as usize;
        let Some(item) = rest.get(4..4 + n) else { break };
        rest = &rest[4 + n..];
        let line = std::panic::catch_unwind(|| answer(item)).unwrap_or_else(|_| "PANIC".into());
        if writeln!(out, "{line}").is_err() {
            return;
        }
    }
}
