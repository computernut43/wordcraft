// Reference program for internal/doc/rust_props_test.go: build with `cargo build --release` in
// the oracle crate. Reads inputs from stdin, each a little-endian u32 length and that many bytes:
// one letter naming the operation, then JSON. Prints one line per input: the result written with
// serde_json::to_string_pretty (newlines escaped as \n, backslashes doubled, so it stays one
// line), or ERROR when the JSON doesn't read.
//
// Operations: c, p, t, r, e, x, k, b, d read a CharProps, ParaProps, TableProps, RowProps,
// CellProps, TextEffects, CoreProps, Block or Document and write it back. O, M, U, L, F, W
// take a CharProps pair [a, b] (or one) and write a.overlaid(&b), a.minus(&b), a.unlinked(),
// a.cleared(), a.formatting(), a.without_symbol(); Q takes a ParaProps pair and writes
// a.overlaid(&b); B takes a Borders pair and writes a after a.overlay(&b); E takes a CharProps and
// writes [is_empty, shown_font].
use std::io::{Read, Write};

use serde::Serialize;
use wordcraft_doc::props::{Borders, CellProps, CharProps, ParaProps, RowProps, TableProps};
use wordcraft_doc::wordart::TextEffects;
use wordcraft_doc::{Block, CoreProps, Document};

fn show<T: Serialize>(v: &T) -> String {
    serde_json::to_string_pretty(v).map(|s| s.replace('\\', "\\\\").replace('\n', "\\n")).unwrap_or_else(|e| format!("SERIALIZE {e}"))
}

fn read<T: serde::de::DeserializeOwned + Serialize>(json: &[u8]) -> String {
    serde_json::from_slice::<T>(json).map(|v| show(&v)).unwrap_or_else(|_| "ERROR".into())
}

fn answer(op: u8, json: &[u8]) -> String {
    match op {
        b'c' => read::<CharProps>(json),
        b'p' => read::<ParaProps>(json),
        b't' => read::<TableProps>(json),
        b'r' => read::<RowProps>(json),
        b'e' => read::<CellProps>(json),
        b'x' => read::<TextEffects>(json),
        b'k' => read::<CoreProps>(json),
        b'b' => read::<Block>(json),
        b'd' => read::<Document>(json),
        b'O' | b'M' => match serde_json::from_slice::<(CharProps, CharProps)>(json) {
            Ok((a, b)) => show(&if op == b'O' { a.overlaid(&b) } else { a.minus(&b) }),
            Err(_) => "ERROR".into(),
        },
        b'U' | b'L' | b'F' | b'W' | b'E' => match serde_json::from_slice::<CharProps>(json) {
            Ok(a) => match op {
                b'U' => show(&a.unlinked()),
                b'L' => show(&a.cleared()),
                b'F' => show(&a.formatting()),
                b'W' => show(&a.without_symbol()),
                _ => show(&(a.is_empty(), a.shown_font())),
            },
            Err(_) => "ERROR".into(),
        },
        b'Q' => match serde_json::from_slice::<(ParaProps, ParaProps)>(json) {
            Ok((a, b)) => show(&a.overlaid(&b)),
            Err(_) => "ERROR".into(),
        },
        b'B' => match serde_json::from_slice::<(Borders, Borders)>(json) {
            Ok((mut a, b)) => {
                a.overlay(&b);
                show(&a)
            }
            Err(_) => "ERROR".into(),
        },
        _ => "BAD OP".into(),
    }
}

fn main() {
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
        let line = match item.split_first() {
            Some((&op, json)) => answer(op, json),
            None => "BAD OP".into(),
        };
        if writeln!(out, "{line}").is_err() {
            return;
        }
    }
}
