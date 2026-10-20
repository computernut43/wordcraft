// Reference program for internal/doc/rust_diff_test.go: build with `cargo build --release` here.
// Reads one JSON request per line, {"para": <Paragraph>, "op": …, …}, runs the operation with
// crates/doc and prints one JSON answer per line: {"para": …}, {"props": …}, {"off": n} or
// {"err": "…"}. A request that doesn't parse answers {"parse": "…"}.
use serde_json::{Value, json};
use std::io::{BufRead, Write};
use wordcraft_doc::Paragraph;
use wordcraft_doc::props::CharProps;

fn n(v: &Value, k: &str) -> usize {
    v.get(k).and_then(Value::as_u64).unwrap_or(0) as usize
}

fn answer(req: &Value) -> Value {
    let mut p: Paragraph = match serde_json::from_value(req["para"].clone()) {
        Ok(p) => p,
        Err(e) => return json!({"parse": e.to_string()}),
    };
    let done = |r: wordcraft_doc::Result<()>, p: &Paragraph| match r {
        Ok(()) => json!({"para": p}),
        Err(e) => json!({"err": e.to_string()}),
    };
    match req["op"].as_str().unwrap_or("") {
        "roundtrip" => json!({"para": p}),
        "insert" => {
            let props: CharProps = serde_json::from_value(req["props"].clone()).unwrap_or_default();
            let r = p.insert_text(n(req, "off"), req["text"].as_str().unwrap_or(""), &props).map(|_| ());
            done(r, &p)
        }
        "delete" => {
            let r = p.delete(n(req, "a"), n(req, "b"));
            done(r, &p)
        }
        "format" => {
            let set: CharProps = serde_json::from_value(req["set"].clone()).unwrap_or_default();
            let r = p.format(n(req, "a"), n(req, "b"), &|c: &mut CharProps| {
                if set.bold.is_some() {
                    c.bold = set.bold;
                }
                if set.bold_cs.is_some() {
                    c.bold_cs = set.bold_cs;
                }
                if set.italic.is_some() {
                    c.italic = set.italic;
                }
                if set.size.is_some() {
                    c.size = set.size;
                }
                if set.style.is_some() {
                    c.style = set.style.clone();
                }
            });
            done(r, &p)
        }
        "normalize" => {
            p.normalize();
            json!({"para": p})
        }
        "propsAt" => json!({"props": p.props_at(n(req, "off"))}),
        "propsOfChar" => json!({"props": p.props_of_char(n(req, "off"))}),
        "clamp" => json!({"off": p.clamp(n(req, "off"))}),
        _ => json!({"parse": "unknown op"}),
    }
}

fn main() {
    let stdin = std::io::stdin();
    let mut out = std::io::BufWriter::new(std::io::stdout());
    for line in stdin.lock().lines() {
        let line = line.unwrap_or_default();
        let a = match serde_json::from_str::<Value>(&line) {
            Ok(req) => answer(&req),
            Err(e) => json!({"parse": e.to_string()}),
        };
        let _ = writeln!(out, "{a}");
    }
}
