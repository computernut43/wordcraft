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
//
// Sections and settings: s, g, P, C, R, K, S, X, T read a SectionProps, Settings, Part, Comment,
// Revision, BuildingBlock, Source, CustomProp or TextBody and write it back. N takes
// [NumFormat, n] and writes format(n) and ooxml(); m takes [SectionProps, font size] and writes
// its measurements, column boxes and the section turned landscape/portrait; A takes
// [SectionProps, SectionProps] and writes a after a.absorb(&b); G takes [pitch, font size] and
// writes DocGrid::char_space_for; I takes a TextBody and writes insets_pt().
//
// Styles, lists, tables and content controls: y, n, a, h read a StyleSheet, Numbering, Table or
// ContentControl and write it back. Y takes [StyleSheet, id, name] and writes what the sheet's
// lookups give (get, find, gallery, chain, indents_over_list, table_style, new_id) and the sheet
// after remove(id) and after add_default_gallery(). H takes [Style, TablePart, PartFormat] and
// writes every table_part, then the style after set_table_part. z takes [TableLook, band size,
// row, rows, grid column, span, columns] and writes PartSet::of_cell's parts. Z takes
// [Numbering, ops] and runs the ops (next_label, values, add_list, restart, find_kind,
// shows_label, level) in order, writing each result (compact JSON) and the numbering after them.
// l takes [ListKind, text] and writes levels_for, ListKind::parse and is_bullet. j takes
// [Table, ops] and runs table edits and queries in order, writing each result and the table. w
// takes a string and writes the id StyleSheet::empty().new_id gives it.
//
// Inline objects: o reads an InlineObject and writes it back. i takes [InlineObject, w, h, alt
// text] and writes what its accessors give (is_drawing, frame, canvas_style, text_boxes,
// is_marker, alt_text, is_decorative, is_floating, ink, as_note_separator, plain_text) and the
// object after set_size(w, h), and after set_alt_text. u takes [text, a, b, c, corner, points]
// and writes the lookups and small helpers of the drawing types (ids of charts, legends, SmartArt
// layouts and colours, ink tools, dashes and wrap sides; corner_radius, ruby offsets, cap_alt,
// wrap polygons; Float::effect_extent).
//
// A panic is answered PANIC.
use std::io::{Read, Write};

use serde::Serialize;
use wordcraft_doc::props::{Borders, CellProps, CharProps, ParaProps, RowProps, TableProps};
use wordcraft_doc::section::{DocGrid, NumFormat, SectionProps};
use wordcraft_doc::wordart::TextEffects;
use wordcraft_doc::numbering::{Counters, levels_for};
use wordcraft_doc::props::TableLook;
use wordcraft_doc::styles::{PartFormat, PartSet, TablePart};
use wordcraft_doc::{
    Block, BuildingBlock, Comment, ContentControl, CoreProps, CustomProp, Document, InlineObject, ListKind, Numbering, Part, Revision, Settings, Source,
    Style, StyleSheet, Table, TextBody,
};

fn object_ops(mut o: InlineObject, w: f32, h: f32, alt: &str) -> String {
    let looked = (
        o.is_drawing(),
        o.frame().map(|(w, h, f)| (w, h, f.clone())),
        o.canvas_style().copied(),
        o.text_boxes(),
        o.is_marker(),
        o.alt_text().to_string(),
        o.is_decorative(),
        o.is_floating(),
        o.ink().cloned(),
        o.as_note_separator(),
        o.plain_text().to_string(),
        o.text_box(),
        o.is_canvas(),
    );
    let mut sized = o.clone();
    sized.set_size(w, h);
    let set = o.set_alt_text(alt);
    show(&(looked, sized, set, o))
}

fn helpers(s: &str, a: f32, b: f32, c: f32, corner: Option<u32>, pts: Vec<(i64, i64)>) -> String {
    use wordcraft_doc::chart::{ChartType, LegendPos};
    use wordcraft_doc::connector::corner_radius;
    use wordcraft_doc::freeform::InkTool;
    use wordcraft_doc::line_dash::LineDash;
    use wordcraft_doc::para::{Float, PTabAlign, PTabBase, RubyAlign, WrapText, cap_alt, ruby_offset, ruby_raise};
    use wordcraft_doc::smart_art::{SmartArtColors, SmartArtLayout};
    use wordcraft_doc::wrap::WrapPolygon;
    let ids = (
        ChartType::from_id(s).map(|t| (t, t.id(), t.label(), t.is_round())),
        LegendPos::from_id(s).map(|p| (p, p.id())),
        SmartArtLayout::from_id(s).map(|l| (l, l.id(), l.label(), l.category(), l.description())),
        SmartArtColors::from_id(s).map(|c| (c, c.id(), c.label(), c.accent(a as usize))),
        InkTool::parse(s).map(|t| (t, t.name(), t.default_color(), t.default_width(), t.alpha())),
        (LineDash::from_ooxml(s), LineDash::from_ooxml(s).ooxml(), LineDash::from_ooxml(s).is_solid()),
        WrapText::from_ooxml(s).map(|w| (w, w.ooxml())),
        PTabAlign::from_ooxml(s),
        PTabBase::from_ooxml(s),
        RubyAlign::from_ooxml(s).map(|r| (r, r.ooxml())),
    );
    let float = Float { effect: [a, b, c, -a], ..Float::default() };
    let unit: Vec<(f32, f32)> = pts.iter().map(|&(x, y)| (x as f32 / 1000.0, y as f32 / 1000.0)).collect();
    let numbers = (
        corner_radius(corner, a, b),
        ruby_offset(a, b, c),
        ruby_raise(a, b, c),
        cap_alt(s).chars().count(),
        float.effect_extent(),
        WrapPolygon::new(a > 0.0, pts.clone()),
        WrapPolygon::from_unit(b > 0.0, &unit),
        WrapPolygon::new(false, pts).map(|p| p.unit()),
    );
    show(&(ids, numbers))
}

fn compact<T: Serialize>(v: &T) -> String {
    serde_json::to_string(v).unwrap_or_else(|e| format!("SERIALIZE {e}"))
}

fn ids(v: &[&Style]) -> Vec<String> {
    v.iter().map(|s| s.id.clone()).collect()
}

fn sheet_ops(mut sheet: StyleSheet, id: &str, name: &str) -> String {
    let looked = (
        sheet.get(id).map(|s| s.id.clone()),
        sheet.find(name).map(|s| s.id.clone()),
        ids(&sheet.gallery()),
        ids(&sheet.chain(id)),
        sheet.indents_over_list(id),
        sheet.table_style(id).map(|t| (t.para, t.chr, t.parts)),
        sheet.new_id(name),
    );
    let mut removed = sheet.clone();
    let gone = removed.remove(id).map(|s| s.id);
    sheet.add_default_gallery();
    show(&(looked, gone, removed, sheet))
}

fn numbering_ops(mut n: Numbering, ops: Vec<(u8, u32, u8, Option<ListKind>)>) -> String {
    let mut c = Counters::default();
    let mut out = Vec::new();
    for (op, num, level, kind) in ops {
        let kind = kind.unwrap_or(ListKind::Bullet);
        out.push(match op {
            0 => compact(&c.next_label(&n, num, level)),
            1 => compact(&c.values(&n, num)),
            2 => compact(&n.add_list(kind)),
            3 => compact(&n.restart(num)),
            4 => compact(&n.find_kind(kind)),
            5 => compact(&n.shows_label(num, level)),
            _ => compact(&n.level(num, level)),
        });
    }
    show(&(out, n))
}

fn table_ops(mut t: Table, ops: Vec<(u8, usize, usize, usize, usize)>) -> String {
    let mut out = Vec::new();
    for (op, a, b, c, d) in ops {
        out.push(match op {
            0 => compact(&t.insert_row(a, b)),
            1 => compact(&t.delete_row(a)),
            2 => compact(&t.insert_col(a)),
            3 => compact(&t.delete_col(a)),
            4 => compact(&t.merge_right(a, b, c)),
            5 => compact(&t.merge(a, b, c, d)),
            6 => compact(&t.split_cell(a, b, c)),
            7 => compact(&t.split_cell_grid(a, b, c, d)),
            8 => compact(&t.vertical_extent(a, b)),
            9 => compact(&t.cell_at_grid(a, b)),
            10 => compact(&t.grid_col(a, b)),
            11 => compact(&t.cols()),
            12 => compact(&t.total_width()),
            _ => {
                t = Table::new(a, b, c as f32);
                compact(&())
            }
        });
    }
    show(&(out, t))
}

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
        b's' => read::<SectionProps>(json),
        b'g' => read::<Settings>(json),
        b'P' => read::<Part>(json),
        b'C' => read::<Comment>(json),
        b'R' => read::<Revision>(json),
        b'K' => read::<BuildingBlock>(json),
        b'S' => read::<Source>(json),
        b'X' => read::<CustomProp>(json),
        b'T' => read::<TextBody>(json),
        b'N' => match serde_json::from_slice::<(NumFormat, u32)>(json) {
            Ok((f, n)) => show(&(f.format(n), f.ooxml(), NumFormat::from_ooxml(f.ooxml()))),
            Err(_) => "ERROR".into(),
        },
        b'm' => match serde_json::from_slice::<(SectionProps, f32)>(json) {
            Ok((mut s, size)) => {
                let measured = (
                    s.text_width(),
                    s.text_height(),
                    s.grid_line_pitch(),
                    s.grid_char_pitch(size),
                    s.grid_lines_per_page(),
                    s.grid_chars_per_line(size),
                    s.column_boxes(),
                );
                let landscape = !s.landscape;
                s.set_landscape(landscape);
                show(&(measured, s))
            }
            Err(_) => "ERROR".into(),
        },
        b'A' => match serde_json::from_slice::<(SectionProps, SectionProps)>(json) {
            Ok((mut a, b)) => {
                a.absorb(&b);
                show(&a)
            }
            Err(_) => "ERROR".into(),
        },
        b'G' => match serde_json::from_slice::<(f32, f32)>(json) {
            Ok((pitch, size)) => show(&DocGrid::char_space_for(pitch, size)),
            Err(_) => "ERROR".into(),
        },
        b'I' => match serde_json::from_slice::<TextBody>(json) {
            Ok(t) => show(&t.insets_pt()),
            Err(_) => "ERROR".into(),
        },
        b'y' => read::<StyleSheet>(json),
        b'n' => read::<Numbering>(json),
        b'a' => read::<Table>(json),
        b'h' => read::<ContentControl>(json),
        b'Y' => match serde_json::from_slice::<(StyleSheet, String, String)>(json) {
            Ok((sheet, id, name)) => sheet_ops(sheet, &id, &name),
            Err(_) => "ERROR".into(),
        },
        b'H' => match serde_json::from_slice::<(Style, TablePart, PartFormat)>(json) {
            Ok((mut st, p, f)) => {
                let parts: Vec<PartFormat> = TablePart::ALL.iter().map(|&q| st.table_part(q)).collect();
                st.set_table_part(p, f);
                show(&(parts, st))
            }
            Err(_) => "ERROR".into(),
        },
        b'z' => match serde_json::from_slice::<(TableLook, u32, usize, usize, usize, usize, usize)>(json) {
            Ok((look, band, ri, nrows, g, span, ncols)) => {
                show(&PartSet::of_cell(&look, band, (ri, nrows), (g, span, ncols)).iter().collect::<Vec<_>>())
            }
            Err(_) => "ERROR".into(),
        },
        b'Z' => match serde_json::from_slice::<(Numbering, Vec<(u8, u32, u8, Option<ListKind>)>)>(json) {
            Ok((n, ops)) => numbering_ops(n, ops),
            Err(_) => "ERROR".into(),
        },
        b'l' => match serde_json::from_slice::<(ListKind, String)>(json) {
            Ok((k, s)) => show(&(levels_for(k), ListKind::parse(&s), k.is_bullet())),
            Err(_) => "ERROR".into(),
        },
        b'j' => match serde_json::from_slice::<(Table, Vec<(u8, usize, usize, usize, usize)>)>(json) {
            Ok((t, ops)) => table_ops(t, ops),
            Err(_) => "ERROR".into(),
        },
        b'o' => read::<InlineObject>(json),
        b'i' => match serde_json::from_slice::<(InlineObject, f32, f32, String)>(json) {
            Ok((o, w, h, alt)) => object_ops(o, w, h, &alt),
            Err(_) => "ERROR".into(),
        },
        b'u' => match serde_json::from_slice::<(String, f32, f32, f32, Option<u32>, Vec<(i64, i64)>)>(json) {
            Ok((s, a, b, c, corner, pts)) => helpers(&s, a, b, c, corner, pts),
            Err(_) => "ERROR".into(),
        },
        b'w' => match serde_json::from_slice::<String>(json) {
            Ok(s) => show(&StyleSheet::empty().new_id(&s)),
            Err(_) => "ERROR".into(),
        },
        _ => "BAD OP".into(),
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
        let line = match item.split_first() {
            Some((&op, json)) => std::panic::catch_unwind(|| answer(op, json)).unwrap_or_else(|_| "PANIC".into()),
            None => "BAD OP".into(),
        };
        if writeln!(out, "{line}").is_err() {
            return;
        }
    }
}
