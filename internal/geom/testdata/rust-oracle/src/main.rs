// Reference program for internal/geom/rust_diff_test.go: build with `cargo build --release` here.
// Reads one operation per line (space-separated; floats as f32 bit patterns in hex, strings as
// hex-encoded UTF-8) and prints the Rust crate's answer per line (floats as bit patterns).
use std::io::{BufRead, Write};
use wordcraft_geom::*;

fn f(s: &str) -> f32 {
    f32::from_bits(u32::from_str_radix(s, 16).unwrap_or(0))
}
fn b(v: f32) -> String {
    format!("{:08x}", v.to_bits())
}
fn s(h: &str) -> String {
    let bytes: Vec<u8> = (0..h.len() / 2).filter_map(|i| u8::from_str_radix(h.get(2 * i..2 * i + 2)?, 16).ok()).collect();
    String::from_utf8(bytes).unwrap_or_default()
}
fn hex(v: &str) -> String {
    v.bytes().map(|c| format!("{c:02x}")).collect()
}
fn unit(i: &str) -> Unit {
    [Unit::Inches, Unit::Centimeters, Unit::Millimeters, Unit::Points, Unit::Picas][i.parse::<usize>().unwrap_or(0) % 5]
}
fn flag(v: &str) -> bool {
    v == "1"
}

fn main() {
    let stdin = std::io::stdin();
    let mut out = std::io::BufWriter::new(std::io::stdout());
    for line in stdin.lock().lines() {
        let line = line.unwrap_or_default();
        let a: Vec<&str> = line.split(' ').collect();
        let g = |i: usize| a.get(i).copied().unwrap_or("");
        let r = match g(0) {
            "twips" => b(twips(g(1).parse().unwrap_or(0))),
            "to_twips" => to_twips(f(g(1))).to_string(),
            "emu" => b(emu(g(1).parse().unwrap_or(0))),
            "to_emu" => to_emu(f(g(1))).to_string(),
            "half" => b(half_points(g(1).parse().unwrap_or(0))),
            "to_half" => to_half_points(f(g(1))).to_string(),
            "number" => hex(&unit(g(1)).number(f(g(2)), s(g(3)).chars().next().unwrap_or('.'))),
            "parse" => parse_measure(&s(g(2)), unit(g(1))).map(b).unwrap_or_else(|| "none".into()),
            "norm" => b(normalize_degrees(f(g(1)))),
            "spin" => {
                let sp = Spin::new(f(g(1)), flag(g(2)), flag(g(3)));
                let (cx, cy, x, y) = (f(g(4)), f(g(5)), f(g(6)), f(g(7)));
                let r = Rect::new(f(g(8)), f(g(9)), f(g(10)), f(g(11)));
                let m = sp.matrix(cx, cy);
                let ap = sp.apply(cx, cy, x, y);
                let un = sp.unapply(cx, cy, x, y);
                let bo = sp.bounds(r);
                let ex = sp.extent(r.w, r.h);
                let mut v: Vec<String> = m.iter().map(|&x| b(x)).collect();
                v.extend([b(ap.0), b(ap.1), b(un.0), b(un.1), b(bo.x), b(bo.y), b(bo.w), b(bo.h), b(ex.0), b(ex.1), b(sp.deg)]);
                v.push(sp.is_identity().to_string());
                v.join(" ")
            }
            "within" => {
                let w = Spin::new(f(g(1)), flag(g(2)), flag(g(3))).within(Spin::new(f(g(4)), flag(g(5)), flag(g(6))));
                format!("{} {} {}", b(w.deg), w.flip_h, w.flip_v)
            }
            "rect" => {
                let p = Rect::new(f(g(1)), f(g(2)), f(g(3)), f(g(4)));
                let q = Rect::new(f(g(5)), f(g(6)), f(g(7)), f(g(8)));
                let u = p.union(&q);
                let i = p.inset(f(g(11)), f(g(12)), f(g(13)), f(g(14)));
                let e = p.expand(f(g(15)));
                let r4 = |r: Rect| format!("{} {} {} {}", b(r.x), b(r.y), b(r.w), b(r.h));
                format!("{} {} {} {} {} {} {}", r4(p), r4(u), p.intersects(&q), p.contains(Point::new(f(g(9)), f(g(10)))), r4(i), r4(e), b(p.right()))
            }
            "paper_name" => paper_name(f(g(1)), f(g(2))).map(hex).unwrap_or_else(|| "none".into()),
            "paper_by" => paper_by_name(&s(g(1))).map(|(n, w, h)| format!("{} {} {}", hex(n), b(w), b(h))).unwrap_or_else(|| "none".into()),
            "fit" => {
                let p = fit_to_paper(f(g(1)), f(g(2)), f(g(3)), f(g(4)));
                format!("{} {} {} {} {}", b(p.paper_w), b(p.paper_h), b(p.k), b(p.dx), b(p.dy))
            }
            "pix" => {
                let (w, h) = (g(1).parse().unwrap_or(0), g(2).parse().unwrap_or(0));
                let (budget, max) = (g(3).parse().unwrap_or(0), g(4).parse().unwrap_or(0));
                let (fw, fh) = fit_pixels(w, h, max);
                let bytes = rgba_bytes(w, h).map(|v| v.to_string()).unwrap_or_else(|| "none".into());
                format!("{bytes} {} {} {fw} {fh}", rgba_fits(w, h, budget), decode_fits(w, h, budget))
            }
            "region" => {
                let mut r = Region::from_tag(&s(g(1)));
                if g(2) != "-" {
                    r = r.with_territory(&s(g(2)));
                }
                let opt = |v: &str| if v == "-" { None } else { Some(s(v)) };
                let (paper, meas) = (opt(g(3)), opt(g(4)));
                r = r.with_paper_and_measurement(paper.as_deref(), meas.as_deref());
                let p = r.paper();
                format!("{} {} {:?} {:?} {} {:?} {}", hex(&r.language), hex(&r.territory), r.letter, r.inches, p.0, r.units(), r.decimal())
            }
            _ => "?".into(),
        };
        let _ = writeln!(out, "{r}");
    }
}
