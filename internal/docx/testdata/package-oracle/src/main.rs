//! Reads inputs from stdin, each a little-endian u64 part limit, u64 total limit, u32 length and
//! that many bytes of zip, and prints one JSON line per input: the package `Package::open_limited`
//! reads (for each zip entry name: the part's size and FNV-1a hash, or null), the relationships `Package::rels` reads for the
//! root and each part, and the content type `ContentTypes::of` gives each part; or the error kind.

#[allow(dead_code)]
#[path = "../../../../../crates/docx/src/xml.rs"]
mod xml;
#[allow(dead_code)]
#[path = "../../../../../crates/docx/src/package.rs"]
mod package;

use std::io::{Read, Write};

/// The variants of the crate's error that package.rs and xml.rs use.
#[allow(dead_code)]
#[derive(Debug)]
pub enum DocxError {
    Zip(String),
    Xml(String),
    Limit(String),
}

impl std::fmt::Display for DocxError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}

fn fnv(b: &[u8]) -> String {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for &x in b {
        h = (h ^ u64::from(x)).wrapping_mul(0x0100_0000_01b3);
    }
    format!("{h:016x}")
}

fn answer(max_part: u64, max_total: u64, zip: &[u8]) -> serde_json::Value {
    let pkg = match package::Package::open_limited(zip, max_part, max_total) {
        Ok(p) => p,
        // "msg" is for diagnosis only; the comparison ignores it.
        Err(DocxError::Zip(m)) => return serde_json::json!({"error": "zip", "msg": m}),
        Err(DocxError::Limit(m)) => return serde_json::json!({"error": "limit", "msg": m}),
        Err(DocxError::Xml(m)) => return serde_json::json!({"error": "xml", "msg": m}),
    };
    // The archive's entry names, in order (the zip crate keeps one per name): which of them the
    // package holds, and what.
    let names: Vec<String> = match zip::ZipArchive::new(std::io::Cursor::new(zip)) {
        Ok(z) => z.file_names().map(str::to_string).collect(),
        Err(_) => Vec::new(),
    };
    let files: Vec<_> = names
        .iter()
        .map(|n| match pkg.get(n) {
            Some(b) => serde_json::json!([n, b.len(), fnv(b)]),
            None => serde_json::json!([n, null, null]),
        })
        .collect();
    let mut rels = serde_json::Map::new();
    let ct = package::ContentTypes::read(&pkg);
    let mut cts = serde_json::Map::new();
    for n in std::iter::once("").chain(names.iter().map(String::as_str)).chain(["word/document.xml", "/WORD/Document.XML", "x.PNG", "noext"]) {
        let list: Vec<_> = pkg.rels(n).list.iter().map(|r| serde_json::json!([r.id, r.kind, r.target, r.external])).collect();
        rels.insert(n.to_string(), list.into());
        cts.insert(n.to_string(), ct.of(n).into());
    }
    serde_json::json!({"files": files, "rels": rels, "ct": cts})
}

/// With ORACLE_LOG set, the package's warnings go to stderr (for diagnosis).
struct StderrLog;

impl log::Log for StderrLog {
    fn enabled(&self, _: &log::Metadata) -> bool {
        true
    }
    fn log(&self, r: &log::Record) {
        eprintln!("{}", r.args());
    }
    fn flush(&self) {}
}

fn main() {
    if std::env::var_os("ORACLE_LOG").is_some() {
        let _ = log::set_logger(&StderrLog);
        log::set_max_level(log::LevelFilter::Warn);
    }
    let mut input = Vec::new();
    std::io::stdin().read_to_end(&mut input).unwrap();
    let mut out = std::io::BufWriter::new(std::io::stdout().lock());
    let mut rest = &input[..];
    while rest.len() >= 20 {
        let max_part = u64::from_le_bytes(rest[0..8].try_into().unwrap());
        let max_total = u64::from_le_bytes(rest[8..16].try_into().unwrap());
        let n = u32::from_le_bytes(rest[16..20].try_into().unwrap()) as usize;
        let item = &rest[20..20 + n];
        rest = &rest[20 + n..];
        writeln!(out, "{}", answer(max_part, max_total, item)).unwrap();
    }
}
