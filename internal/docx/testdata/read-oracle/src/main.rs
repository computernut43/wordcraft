//! The main program. Reads inputs from stdin, each a little-endian u32 length and that many bytes of a .docx
//! package, and prints one line per input: the document `wordcraft_docx::read` makes of it,
//! written with to_string_pretty (newlines escaped as \n, backslashes doubled), or `ERR <kind>`
//! with the DocxError variant, or PANIC.
use std::io::{Read, Write};

fn answer(bytes: &[u8]) -> String {
    match wordcraft_docx::read(bytes) {
        Ok(d) => serde_json::to_string_pretty(&d).map(|s| s.replace('\\', "\\\\").replace('\n', "\\n")).unwrap_or_else(|e| format!("SERIALIZE {e}")),
        Err(e) => {
            let dbg = format!("{e:?}");
            let kind = dbg.split(['(', ' ']).next().unwrap_or("").to_string();
            format!("ERR {kind}")
        }
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
