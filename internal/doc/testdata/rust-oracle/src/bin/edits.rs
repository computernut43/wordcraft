// Reference program for internal/doc/rust_edits_test.go: build with `cargo build --release` in the
// oracle crate. Reads inputs from stdin, each a little-endian u32 length and that many bytes of
// JSON: [Document, [op, …]]. Runs the ops on the document in order and prints one line per input:
// the result of each op as compact JSON, then the document, written with to_string_pretty
// (newlines escaped as \n, backslashes doubled), or ERROR when the input doesn't read. An op is an
// object {"op": name, …}; see `run`. A panic is answered PANIC.
use std::io::{Read, Write};

use serde::Serialize;
use serde_json::{Value, json};
use wordcraft_doc::edit::Fragment;
use wordcraft_doc::para::OBJ;
use wordcraft_doc::props::{Align, CharProps};
use wordcraft_doc::{Block, Document, InlineObject, Paragraph, PartKind, Path, Pos, StoryRef};

fn show<T: Serialize>(v: &T) -> String {
    serde_json::to_string_pretty(v).map(|s| s.replace('\\', "\\\\").replace('\n', "\\n")).unwrap_or_else(|e| format!("SERIALIZE {e}"))
}

fn compact<T: Serialize>(v: &T) -> String {
    serde_json::to_string(v).unwrap_or_else(|e| format!("SERIALIZE {e}"))
}

fn get<T: serde::de::DeserializeOwned>(v: &Value, k: &str) -> Option<T> {
    serde_json::from_value(v.get(k)?.clone()).ok()
}

fn res<T: Serialize>(r: wordcraft_doc::Result<T>) -> String {
    match r {
        Ok(v) => format!("{{\"ok\":{}}}", compact(&v)),
        Err(e) => compact(&json!({ "err": e.to_string() })),
    }
}

fn field(f: &wordcraft_doc::FieldRange) -> (String, bool, Pos, Pos, usize) {
    (f.instr.clone(), f.locked, f.start.clone(), f.end.clone(), f.depth)
}

fn control(r: &wordcraft_doc::ControlRange) -> (wordcraft_doc::ContentControl, Pos, Pos, usize) {
    (r.control.clone(), r.start.clone(), r.end.clone(), r.depth)
}

fn run(d: &mut Document, frag: &mut Fragment, op: &Value) -> String {
    let pos = |k: &str| get::<Pos>(op, k).unwrap_or_default();
    let story = get::<StoryRef>(op, "story").unwrap_or_default();
    let path = get::<Path>(op, "path").unwrap_or_default();
    let n = |k: &str| op.get(k).and_then(Value::as_u64).unwrap_or(0) as usize;
    let b = |k: &str| op.get(k).and_then(Value::as_bool).unwrap_or(false);
    let s = |k: &str| op.get(k).and_then(Value::as_str).unwrap_or("").to_string();
    let props = get::<CharProps>(op, "props").unwrap_or_default();
    match op.get("op").and_then(Value::as_str).unwrap_or("") {
        "insertText" => res(d.insert_text(&pos("a"), &s("text"), &props)),
        "insertObject" => match get::<InlineObject>(op, "obj") {
            Some(o) => res(d.insert_object(&pos("a"), o, &props)),
            None => "BAD".into(),
        },
        "split" => res(d.split_paragraph(&pos("a"))),
        "insertBlock" => match get::<Block>(op, "block") {
            Some(blk) => res(d.insert_block(story, &path, blk)),
            None => "BAD".into(),
        },
        "removeBlock" => res(d.remove_block(story, &path)),
        "delete" => res(d.delete_range(&pos("a"), &pos("b"))),
        "format" => {
            let bold = b("bold");
            res(d.format_range(&pos("a"), &pos("b"), &|c: &mut CharProps| {
                c.bold = Some(bold);
                if bold {
                    c.link = Some("x".into());
                }
            }))
        }
        "formatParas" => {
            let center = b("center");
            res(d.format_paragraphs(&pos("a"), &pos("b"), &|p| p.align = Some(if center { Align::Center } else { Align::Right })))
        }
        "copy" => {
            *frag = d.copy_range(&pos("a"), &pos("b"));
            compact(&*frag)
        }
        "bodyFragment" => {
            *frag = d.body_fragment();
            compact(&*frag)
        }
        "paste" => res(d.insert_fragment(&pos("a"), frag)),
        "setStory" => match get::<Vec<Block>>(op, "blocks") {
            Some(bl) => res(d.set_story(story, bl.into_iter().map(std::sync::Arc::new).collect())),
            None => "BAD".into(),
        },
        "restyle" => compact(&d.restyle(&s("from"), get::<Option<String>>(op, "to").flatten())),
        "convertNotes" => compact(&d.convert_notes(b("foot"), b("end"))),
        "clamp" => compact(&d.clamp(&pos("a"))),
        "paths" => compact(&(
            d.para_paths(story),
            d.paths_between(&pos("a"), &pos("b")),
            d.para_paths_in(&pos("a"), &pos("b")),
            d.start_of(story),
            d.end_of(story),
            d.next_para(story, &path),
            d.prev_para(story, &path),
        )),
        "fields" => compact(&(
            d.field_ranges(story).iter().map(field).collect::<Vec<_>>(),
            d.past_field_ends(&pos("a")),
            d.has_unbalanced_field_ranges(),
        )),
        "balanceFields" => compact(&d.balance_field_ranges()),
        "controls" => {
            let ranges = d.control_ranges(story);
            compact(&(
                ranges.iter().map(control).collect::<Vec<_>>(),
                d.control_at(&pos("a")).as_ref().map(control),
                ranges.iter().map(|r| (d.control_text(r), r.is_empty(), r.contains(&pos("a")))).collect::<Vec<_>>(),
                d.typing_pos(&pos("a")),
                d.has_unbalanced_controls(),
                d.fillable_controls(),
            ))
        }
        "balanceControls" => compact(&d.balance_controls()),
        "sections" => {
            let secs: Vec<(usize, wordcraft_doc::SectionProps)> = d.sections().into_iter().map(|(i, s)| (i, s.clone())).collect();
            let opts = secs
                .iter()
                .map(|(_, s)| {
                    let f = d.note_options(s, false);
                    let e = d.note_options(s, true);
                    json!([[f.pos, f.num_fmt, f.num_start, f.num_restart], [e.pos, e.num_fmt, e.num_start, e.num_restart]])
                })
                .collect::<Vec<_>>();
            compact(&(secs.len(), d.section_index_of(n("block")), opts))
        }
        "removeSectionBreak" => compact(&d.remove_section_break(n("block"))),
        "sectionMut" => {
            let landscape = b("landscape");
            d.section_mut(n("block")).landscape = landscape;
            compact(&())
        }
        "text" => compact(&(d.plain_text(story), d.final_text(story))),
        "addPart" => {
            let kind = get::<PartKind>(op, "kind").unwrap_or_default();
            let blocks = get::<Vec<Block>>(op, "blocks").unwrap_or_default();
            compact(&d.add_part(kind, blocks.into_iter().map(std::sync::Arc::new).collect()))
        }
        "textBoxAnchor" => compact(&d.text_box_anchor(n("part") as u32)),
        "para" => {
            // Paragraph-level queries and edits on the paragraph at `a`.
            let a = pos("a");
            let Some(p) = d.para_at(&a).cloned() else { return "NOPARA".into() };
            let off = a.off;
            let mut q = p.clone();
            let split = q.split_off(p.clamp(off)).ok();
            let mut joined = q.clone();
            if let Some(t) = split.clone() {
                joined.append(t);
            }
            let mut del = p.clone();
            let keep: Vec<usize> = p.object_offsets();
            let r = del.delete_except(p.clamp(n("x")), p.clamp(n("y")), &keep);
            let ranges = vec![p.clamp(n("x"))..p.clamp(n("y")), p.clamp(off)..p.len()];
            compact(&(
                p.word_at(off),
                p.word_start(off),
                p.word_end(off),
                p.sentence_at(off),
                p.plain_text(),
                p.final_text(),
                p.text_without(&ranges),
                p.object_offsets(),
                p.object_at(p.clamp(off)),
                (split, q, joined),
                (r.map_err(|e| e.to_string()), del),
                p.deleted_ranges(),
            ))
        }
        "fragment" => {
            let mut f = frag.clone();
            let piece = Fragment::from_text(&s("text"));
            f.append_piece(piece);
            let mut g = frag.clone();
            g.append_piece(frag.clone());
            compact(&(f, g, frag.has_equation(), frag.sole_equation(), frag.plain_text()))
        }
        _ => "BAD OP".into(),
    }
}

fn answer(json: &[u8]) -> String {
    let Ok((mut d, ops)) = serde_json::from_slice::<(Document, Vec<Value>)>(json) else { return "ERROR".into() };
    let mut frag = Fragment::default();
    let mut out = Vec::new();
    for op in &ops {
        out.push(run(&mut d, &mut frag, op));
    }
    let _ = OBJ;
    let _ = Paragraph::new;
    show(&(out, d))
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
