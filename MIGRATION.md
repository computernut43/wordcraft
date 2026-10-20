# WordCraft: Rust → Go migration plan

> **Status:** approved 2026-10-20; M0 done; M1 in progress (`geom` verified; document model typed and serde-exact, M1d.1–4; navigation and editing across tables and stories, M1e.1; engine core and CLI M1b; DOCX XML and package M1c.1–2; DOCX reader core, content controls and pictures M1c.3–5) · **Created:** 2026-10-20 · **Rust baseline:** `9b4b948` (computernut43/wordcraft `main`)
>
> This plan covers the migration. Go code lands component by component; no Rust code is to be removed until
> the matching Go code builds, passes its tests and has been reviewed (§9).

## 1. Goal and decisions so far

| Question | Owner's answer (2026-10-20) | What it means for the plan |
|---|---|---|
| End state | Port what is in Rust today to Go first, then keep building a working word processor in Go | A full replacement: engine, formats, layout, render, PDF, UI, desktop and browser. Port first, extend later. |
| Browser | Required, in Go | Every package below the UI must build for `GOOS=js GOARCH=wasm`. A wasm build check is a gate from M0. |
| cgo | Allowed, "but let's be secure about it" | cgo is for OS integration only, and only where `purego` or `x/sys` can't do the job. Any temporary use of Rust from Go goes through a sandboxed subprocess, not cgo (§5). |
| Upstream `storytold/wordcraft` | Not merged any more; the fork goes its own way | The Rust tree is **frozen** at a baseline tag and serves as the reference implementation. There's no drift to track. |
| craft-libs | Port it to Go as well, in the owner's fork [`computernut43/craft-libs`](https://github.com/computernut43/craft-libs) (2026-10-20) | Shared Craft code (PDF writing, printing, UI controls, settings, …) and the shared Go tooling live there as the Go module `github.com/computernut43/craft-libs`; WordCraft requires it by commit. Its own plan: `MIGRATION.md` in that repository. Licence confirmed `MIT OR Apache-2.0`. |
| Where the code goes | The owner's forks (2026-10-20) | All work is pushed to `computernut43/wordcraft` and `computernut43/craft-libs` only, on branch `claude/charming-hopper-wtmlhw` until merged; nothing goes to `storytold`. |
| Where the Go code lives | Undecided ("what is the best way?") | **Recommendation: this repository, with a Go module at the repo root, side by side with the Rust tree until cutover** (§2). |
| The "Rust only" rule and the `cargo xtask ci` gate | To be updated | M0 rewrites `CLAUDE.md`/`AGENTS.md` for the dual-language period and adds Go gates (§7). |

## 2. Repository layout and how the Rust code gets replaced

Recommendation: **one repository, Go module at the root, Rust frozen beside it, removed in one reviewed cutover.**

- `go.mod` sits at the repository root with module path `github.com/computernut43/wordcraft`. Go ignores
  `crates/`, `apps/` and `xtask/` (they contain no `.go` files), and Cargo ignores the Go directories, so both builds
  coexist with no tooling conflict.
- Because the module path is the final one from day one, cutover is a pure deletion: no import paths change and no
  files move. A `go/` subdirectory would force a rename of every import at cutover, and a separate repository
  would split history, issues, `ATTRIBUTION.md`, assets and the tests the port is checked against.
- Shared, language-neutral material stays where it is and is used by both: `assets/` (fonts, spelling,
  hyphenation, icons), `ATTRIBUTION.md`, `NOTICE`, `LICENSE-*`, `docs/`, `packaging/`, and the i18n catalogs
  (`crates/ui-egui/src/i18n/*.tsv`, moved to `assets/i18n/` when the Go UI needs them).
- **Freeze:** the baseline is commit `9b4b948c6ecf29401e01ad58626d3c1983b83770`, pinned by SHA in the Go workflow and the
  agent instructions. (A `rust-baseline` tag is optional; this session's git access can't push tags, so the owner
  can add it on GitHub.) From then on the Rust code changes only to fix bugs that
  break its use as the reference. New features go to Go only.
- **Removal is top-down or all at once.** Rust crates can't be deleted one by one from the bottom, because the Rust
  apps link all of them. Two exits, both safe:
  1. Delete Rust apps/crates in reverse layer order once their Go replacement has passed review: `ui-egui` and `apps/*`
     after the Go UI ships, then `mcp`/`zotero`/`engine`, and so on down. Lower crates stay while anything Rust
     still links them.
  2. Or keep the whole Rust tree until parity (§6) is reached, then remove `crates/ apps/ xtask/ Cargo.toml
     Cargo.lock clippy.toml rustfmt.toml` in one cutover PR, after tagging `rust-final`.
  Recommendation: option 2, plus a tag. It keeps the full reference app runnable for the reference comparison (§8) the
  whole time.

Proposed Go tree (one package per Rust crate, matching its layer):

```
go.mod                       module github.com/computernut43/wordcraft
cmd/
  wordcraft/                 desktop app            (apps/wordcraft)
  wordcraft-cli/             CLI + MCP entry        (apps/wordcraft-cli)
  wordcraft-web/             browser build          (apps/wordcraft-web)
internal/
  geom/                L0    units, rects, measurements      (crates/geom)
  doc/                 L1    document model, edit primitives (crates/doc)
  fonts/               L1    font DB, shaping, substitution  (crates/fonts)
  proof/               L1    spelling, grammar, hyphenation  (crates/proof)
  layout/              L2    layout → display list           (crates/layout)
  docx/                L2    OOXML read/write, encryption    (crates/docx)
  docbin/              L2    .doc reader                     (crates/docbin)
  formats/             L2    txt md html rtf odt tex sheets  (crates/formats)
  metafile/            L1    WMF/EMF                         (crates/metafile)
  cfb/                 L0*   compound file read+write        (new: Rust used the `cfb` crate)
  render/              L3    rasteriser                      (crates/render)
  pdf/                 L3    PDF writer                      (crates/pdf + craft-pdf)
  engine/              L4    Session, undo, commands, I/O    (crates/engine)
  zotero/              L5                                    (crates/zotero)
  controlkey/          L0*                                   (crates/control-key)
  mcp/                 L5                                    (crates/mcp)
  ui/                  L6    front end                       (crates/ui-egui)
  platform/            L6    per-OS integration              (objc2 / windows / zbus code in apps + ui)
  rustref/             test  harness that runs the frozen Rust CLI as a reference (§8); never linked into apps
tools/
  ci/                        Go counterpart of `cargo xtask ci` (gofmt, vet, staticcheck, tests, layers, assets, wasm)
```

Layers follow the Rust gate's table (`xtask/src/layers.rs`), checked 2026-10-20: `metafile` is L1 and `zotero` L5; L0\*
marks *standalone* units, which import no other unit. A unit may import only strictly lower layers, except that
`fonts` may import `doc` (the Rust `INTRA_LAYER_ORDER`); `rustref` is test-only. `tools/ci layers` enforces this.

`internal/` is used because WordCraft is an application. A package can be promoted to a public API later without
changing its import path inside the module. The layering rule (a package imports only lower layers, and nothing
below L6 imports UI or OS packages) is checked by `tools/ci layers` from `go list -deps`.

## 3. Rust → Go component mapping

Sizes are lines including in-file tests, at the baseline. The test count is the number of `#[test]` functions in the crate.

| Rust crate | Lines | Responsibility | Go package | Approach | Difficulty |
|---|---|---|---|---|---|
| `geom` | 0.7k | Units (pt, twip, EMU, in, cm), rects, measurement parsing | `internal/geom` | Straight port | Low |
| `doc` | 20.4k | `Document`, `Block{Para,Table}`, `Paragraph{text, runs, props, objects…}`, styles, numbering, sections, fields, comments, revisions, math, charts, SmartArt, WordArt, bidi helpers, edit primitives | `internal/doc` | Port. JSON shape kept identical to Rust's serde output (§8) | Medium (size) |
| `fonts` | 5.7k | Font DB (bundled OFL and system fonts), metrics, outlines, HarfRust shaping, Word font substitution, OpenType MATH, WOFF/WOFF2 | `internal/fonts` | `go-text/typesetting` for parsing and shaping; port the substitution table; **write** a MATH table parser and a WOFF2 decoder | High |
| `proof` | 13.6k | Spelling, grammar, hyphenation per language; Hunspell-style affix engine; Liang patterns | `internal/proof` | Straight port, pure logic plus data in `assets/` | Medium |
| `layout` | 28.7k | Shaping, line breaking, tabs, lists, tables, pagination, headers/footers, notes, floats and wrap, math, vertical text, kinsoku, hit testing, incremental relayout | `internal/layout` | Port, using go-text `segmenter` (UAX #14/#29) and `bidi` (UAX #9) | **Very high** |
| `docx` | 32.7k | OOXML read/write (Transitional and Strict), Flat OPC, Word 2003 XML, agile encryption, charts, SmartArt, glossary, content controls, passthrough | `internal/docx` | Port. Read with our own bounded tokenizer that splits, resolves and fails exactly as the baseline's quick-xml 0.41 does (`encoding/xml` normalizes line ends in text and doesn't normalize attribute whitespace, so its trees differ from Rust's); write with a hand-written XML writer (exact prefixes and order) | High (size, fidelity) |
| `docbin` | 7.7k | `.doc` reader: FIB, piece table, FKPs, SPRMs, stylesheet, lists, tables, notes, OfficeArt, XOR/RC4 decryption | `internal/docbin` | Port over `internal/cfb` | Medium |
| `formats` | 19.7k | TXT, Markdown, HTML, RTF, ODT, LaTeX, MathML; spreadsheets for mail merge (`.xlsx`, `.ods`, `.xls`) | `internal/formats` | Port; `x/text/encoding` for code pages; port chardetng's detector (Apache-2.0/MIT, attributed) | Medium |
| `metafile` | 3.2k | WMF/EMF → paths | `internal/metafile` | Straight port | Medium |
| `render` | 3.3k | Display list → pixels (vello_cpu), thumbnails, PNG/JPEG | `internal/render` | Spike first (§4): `tdewolff/canvas` rasteriser vs `x/image/vector` vs our own | High |
| `pdf` | 3.6k | Display list → PDF through craft-pdf/krilla: subsets, selectable text, links, outline, tagging | `internal/pdf` | Our own PDF writer (ISO 32000, public) using go-text's font tables for subsetting | High |
| `engine` | 58.5k | `Session`, selection, undo, command registry (**633 commands**), Word feature catalog, samples, I/O dispatch, crash recovery, Read Aloud model, print model | `internal/engine` | Port the framework, then commands namespace by namespace | High (size) |
| `zotero` | 1.7k | Zotero integration protocol | `internal/zotero` | Straight port | Low |
| `control-key` | 0.1k | Per-window control-port keys | `internal/controlkey` | Straight port | Low |
| `mcp` | 1.3k | MCP server: every command as a tool, headless or bridged | `internal/mcp` | Port onto the official `modelcontextprotocol/go-sdk`; keep tool names and schemas identical | Low–medium |
| `ui-egui` | 63.4k | Ribbon, keytips, Backstage, canvas, rulers, panes, ~59 dialogs, 21 i18n catalogs, themes, control channel, a11y, printing UI, **36 `ui.*` commands** | `internal/ui` | Rewrite on the chosen toolkit (§4, spike first). Not a translation: egui and the Go toolkits differ | **Very high** |
| `apps/wordcraft` | 3.6k | eframe/wgpu shell, logging, window geometry, file associations, macOS/Windows/Linux integration, craft-print | `cmd/wordcraft` + `internal/platform` | Rewrite; `purego/objc`, `x/sys/windows`, `godbus/dbus` | High |
| `apps/wordcraft-cli` | 1.8k | `convert info text inspect render run commands parity mcp zotero` | `cmd/wordcraft-cli` | Port with identical flags and output | Low |
| `apps/wordcraft-web` | 0.9k | eframe web runner, IndexedDB, clipboard, downloads | `cmd/wordcraft-web` | Rewrite with `syscall/js` + toolkit wasm backend; `wasm_exec.js` comes from `$GOROOT/lib/wasm` | High |
| `xtask` | 1.4k | ci, layers, assets, parity, wasm, stats, visual diff, icons | `tools/ci` | Port the gates; `parity` regenerates `docs/parity-checklist.md` from the Go catalog | Low–medium |
| git deps `craft-pdf` (+ krilla), `craft-ui`, `craft-print` (craft-libs) | n/a | PDF backend, shared UI pieces, native printing | `github.com/computernut43/craft-libs/{pdf/write,ui,print}`, used by `internal/pdf`, `internal/ui`, `internal/platform` | Ported in the craft-libs fork (its milestones C2, C4, C5); licence `MIT OR Apache-2.0` | High |

## 4. Library decisions

Checked 2026-10-20 against the Go module proxy (`go list -m -versions`, `go mod download` licence files), pkg.go.dev, the projects'
GitHub READMEs and their source code. Versions are the latest on the proxy at that date. Allowed licences:
MIT, BSD, Apache-2.0, ISC, Unlicense, Zlib. **No GPL/LGPL/AGPL code is linked**, matching the clean-room rule.

**Go toolchain:** `go 1.26` in `go.mod`, `toolchain go1.27.3`. Go 1.27.3 is the latest release, and only 1.26 and 1.27 are
supported. The machine's 1.24.7 is out of support, and the MCP SDK below needs Go ≥ 1.25.

### Decided (low risk)

| Need (Rust crate) | Go choice | Licence | Why |
|---|---|---|---|
| ZIP (`zip`), deflate/zlib (`miniz_oxide`) | `archive/zip`, `compress/flate`, `compress/zlib` | BSD (stdlib) | Standard library |
| Crypto (`aes`, `sha1`, `sha2`, `hmac`, `getrandom`, `base64`) | `crypto/aes`, `crypto/cipher`, `crypto/sha1`, `crypto/sha512`, `crypto/hmac`, `crypto/rand`, `encoding/base64` | BSD | Standard library. `zeroize` has no exact equivalent; overwrite password buffers by hand, best effort |
| Regex (`regex`, `regex-automata`) | `regexp` | BSD | Also RE2-style with linear-time matching, the same family as Rust's `regex` |
| JSON (`serde_json`) | `encoding/json` | BSD | Custom `MarshalJSON`/`UnmarshalJSON` where serde uses `tag = "kind"`, `rename_all = "camelCase"` or `skip_serializing_if`. `encoding/json/v2` isn't relied on until it is stable |
| XML (`quick-xml`) | `encoding/xml` (`Decoder.RawToken`/`Token`) to read; hand-written writer | BSD | `encoding/xml` can't control namespace prefixes on output, and OOXML consumers care about them |
| Images (`image`) | `image/png`, `image/jpeg`, `image/gif`, `golang.org/x/image` (`bmp`, `tiff`, `webp` decode) v0.47.0 | BSD | WebP is decode-only in Go. Rust only labels WebP when writing DOCX (`docx/src/write/mod.rs:566`) and never encodes it |
| Text encodings (`encoding_rs`) | `golang.org/x/text/encoding/...` v0.43.0 | BSD | GBK/GB18030, Big5, Shift_JIS, EUC-KR and single-byte code pages all covered |
| Parallelism (`rayon`) | goroutines + `sync`/`errgroup` | BSD | |
| Geometry (`kurbo`) | own small package in `internal/geom` | n/a | Only Béziers, affine transforms and rects are used |
| MCP | `github.com/modelcontextprotocol/go-sdk` v1.8.0 | Apache-2.0 (older parts MIT) | The SDK from the `modelcontextprotocol` GitHub organisation. Stdio, streamable HTTP and SSE transports; generic `AddTool` |
| D-Bus (`zbus`) | `github.com/godbus/dbus/v5` v5.2.4 | BSD-2 | Linux theme and portal integration; pure Go |
| OS calls without cgo (`objc2`, parts of `windows`) | `github.com/ebitengine/purego` v0.11.2 (incl. `purego/objc`) + `golang.org/x/sys` v0.49.0 | Apache-2.0 / BSD | Calls system libraries without cgo: darwin, linux, freebsd and netbsd via `dlopen`, Windows via `syscall`. Keeps cross-compiling simple |
| Compound files (`cfb`), reading | `github.com/richardlehane/mscfb` v1.0.9 | Apache-2.0 | Maintained (Sep 2026) and has a fuzz harness. **Can't create a container**: `Write` only overwrites existing streams. See below |

### Decided, with extra work we own

| Need | Choice | Gap we fill |
|---|---|---|
| Shaping + font parsing (`harfrust`, `skrifa`) | `github.com/go-text/typesetting` v0.3.5 (BSD-3 / Unlicense): `harfbuzz`, `shaping`, `font`, `font/opentype`, `fontscan` | Pure Go, shared by Gio, Fyne and Ebitengine; three maintainers from different projects. Pre-1.0: pin and review each upgrade. **No OpenType MATH table** (we parse it, ~0.5k lines) and **no WOFF2** (only WOFF1 in `reader_woff.go`): we write a WOFF2 decoder, with `github.com/andybalholm/brotli` v1.2.6 (MIT) for the Brotli stream. There are 5 non-test `panic()`s in `segmenter`/`harfbuzz` (4 are "exhaustive switch" guards, 1 in the Arabic fallback); every call goes through a `recover` guard (§7) |
| Bidi (`unicode-bidi`) | `github.com/go-text/typesetting/bidi` | **Not** `golang.org/x/text/unicode/bidi`: that package's own doc comment says "UNDER CONSTRUCTION" and it contains assertion `panic`s. go-text's version has no `panic`s. It is checked against the Unicode BidiTest/BidiCharacterTest files before use |
| Line and grapheme breaking (`unicode-linebreak`, `unicode-segmentation`) | `github.com/go-text/typesetting/segmenter` (UAX #14, UAX #29); `github.com/rivo/uniseg` v0.4.7 (MIT) as a second option | uniseg's last release was 2024-02. It is stable but slow-moving, so it is kept only as a cross-check |
| Compound file writing | Own `internal/cfb` writer (MS-CFB, public spec), reads via mscfb | Needed in production for encrypted `.docx` save (`crates/docx/src/crypt.rs:195`). The other Rust CFB writers are test fixtures only (`sheet::xls_fixture`, `ui-egui/src/file_dialogs.rs` tests), and can use the same writer |
| Charset detection (`chardetng`) | Port chardetng's algorithm into `internal/formats/chardet` with attribution | The Go options found are stale (`saintfish/chardet` 2023-01, `gogs/chardet` 2021-11) |
| PDF (`craft-pdf`/krilla) | Own `internal/pdf` writer | `tdewolff/canvas`'s PDF backend subsets fonts but has no link annotations, outline or tagged structure (`renderers/pdf/*.go`, checked). WordCraft's PDF export needs all three. `signintech/gopdf` v0.38.2 (MIT) is evaluated in the spike as a lower layer |

### Decided by a spike (time-boxed, before the milestone that needs it)

| Need | Candidates | Spike exit criteria |
|---|---|---|
| Rasteriser (`vello_cpu`) | `github.com/tdewolff/canvas` (MIT, pure Go by default; HarfBuzz/FriBidi cgo only with build tags `harfbuzz`/`fribidi`, **which we never set, since FriBidi is LGPL**; untagged pseudo-versions, README asks for funding, author warns of bugs in complex cases) · `golang.org/x/image/vector` (BSD, fills only) · our own | Render the Rust sample's display list. Pixel difference against the Rust PNG within a threshold; ≤ 2× Rust time for a page at 150 dpi; builds for wasm |
| **UI toolkit** (`egui`/`eframe`) | **Gio** `gioui.org` v0.10.3 (MIT / Unlicense): immediate mode like egui, so the UI's structure carries over best; macOS, Windows, Linux, FreeBSD, OpenBSD, Android, iOS; **WebAssembly listed as experimental**; `semantic` package for screen readers; uses go-text · **Fyne** `fyne.io/fyne/v2` v2.8.2: retained mode, more widgets built in; how well its browser target works is checked in the spike. Wails is excluded (webview + JS) | A ribbon tab, the document canvas (pages from `internal/render`), text input with IME (CJK), one modal dialog, keyboard shortcuts and screen-reader names. Must work on macOS, Windows, Linux and in the browser. Pick the toolkit that passes, leaning to Gio |
| File dialogs, clipboard (`rfd`, `arboard`) | The toolkit's own (`gioui.org/x/explorer` for Gio), else `purego` per OS | Part of the UI spike |
| Printing (`craft-print`) | Per OS: `purego` to AppKit `NSPrintOperation` on macOS, Win32 print APIs via `x/sys/windows`, CUPS/portal via `godbus` on Linux | Spike in M10 |

## 5. Security rules for cgo and the temporary Rust bridge

The owner allowed cgo with the condition "let's be secure about it". The plan:

1. **No cgo into Rust in shipped binaries.** Linking a Rust static library through cgo needs `extern "C"` and
   `#[unsafe(no_mangle)]`, which breaks the workspace's `unsafe_code = "forbid"`. It also puts Rust and Go memory
   in one address space, where a bug on either side can corrupt the other. It doesn't work for the browser build either.
2. **The transitional bridge is a subprocess.** Where Go needs a Rust capability before its own port is ready, it runs the frozen
   `wordcraft-cli` as a child process:
   - only inputs and outputs as byte streams (stdin/stdout, JSON or file bytes), never pointers;
   - size caps on what is sent and received, and a timeout on every call;
   - a fixed absolute binary path with a checked SHA-256, no shell, no inherited environment beyond an allowlist;
   - desktop and test builds only; never in wasm, never on by default (`-tags rustbridge`).
   The only planned users are the reference tests (§8). Shipping features through the bridge needs a decision by the owner.
3. **cgo for OS integration only, and last.** Use `purego` / `x/sys` first. When cgo is unavoidable (for example a toolkit
   backend that needs it), the code lives in `internal/platform/<os>`, behind build tags, reviewed line by line. No C
   sources are vendored without an entry in `ATTRIBUTION.md`, and nothing GPL/LGPL is linked.
4. Supply chain: `go.sum` is committed; `govulncheck` runs in CI; dependency upgrades are reviewed PRs (pre-1.0
   libraries especially: go-text, Gio, canvas). Every dependency's licence is recorded in a generated table (`tools/ci licences`).
5. Hostile input stays hostile: every reader has a `go test -fuzz` target, and allocation and recursion are capped
   exactly as the Rust code caps them (e.g. `MAX_TEXT_BOX_DEPTH`, `BoxBudget`).

## 6. Feature parity checklist

Status values: `not started` · `in progress` · `ported` (Go tests pass) · `verified` (checked against the Rust
reference and reviewed) · `gap` (documented as not ported, with the reason). Every row starts `not started`. "Parity" is only claimed
for `verified` rows.

### Model and editing
| Feature | Rust source | Status |
|---|---|---|
| Units, rects, measurement parsing | `crates/geom` | **verified**: all 11 Rust tests ported; 82,706 generated cases compared with the Rust crate bit for bit (`TestMatchesRust`): all identical: sine and cosine come from craft-libs `rustcompat.Sin32`/`Cos32`, a port of the `sinf`/`cosf` Rust calls on Linux x86-64 (glibc's, Arm Optimized Routines' algorithm; before it, 95 rotations differed by one ULP); fuzzed. Signed zeros of `min`/`max` follow Rust on x86-64 |
| Document, paragraphs, runs, character/paragraph properties | `crates/doc/src/{lib,para,props}.rs` | in progress (M1d.1): read and written with craft-libs `serdejson` exactly as serde reads and writes them. **Typed and verified**: every type of `props.rs` (`CharProps`, `ParaProps`, `TableProps`, `RowProps`, `CellProps` and everything in them, with `CharProps` overlay/minus/unlinked/cleared/formatting/without_symbol/shown_font and `ParaProps`/`Borders` overlay), the run text effects of `wordart.rs`/`effects.rs` (data and overlay), `Anchor`/`FloatAlign`, `CoreProps`, `NoteSeparators`, media, `Settings` (with `Zoom`, `PagePicture`, `Watermark`, `MathProps`), `Part`/`TextBody`, `Comment`, `Revision`, `Source`, `CustomProp`, `BuildingBlock`, `Paragraph`, `Run`, `Block`, `Document`: 109,355 generated inputs (valid values of every field, the other forms serde takes, single and double mistakes, the same values inside a block where serde reads them from Content, documents) give the same output byte for byte, or both refuse (`TestPropsMatchRust`, `testdata/rust-oracle` `props`); 35 of 35 planted bugs caught (34 by it, one by a ported unit test); the props.rs unit tests ported; fuzzed (`FuzzUnmarshal`). Paragraph edits (insert, delete, format, normalize, props at an offset) **verified**: 30,000 random edits identical to `crates/doc` (`TestMatchesRust`). Inline objects and a paragraph's section are typed too (M1d.4, below): nothing in the model is kept as untyped JSON any more. Empty parts, empty or table-ended cells of top-level tables, and runs in every story and table are repaired as Rust repairs them. Not ported: `TextFill`/`TextEffects` sanitizing, WordArt styles, text warps |
| Inline objects, drawings, charts, SmartArt, equations (data) | `crates/doc/src/{para,graphic,chart,smart_art,freeform,effects,connector,line_dash,wrap,math}.rs`, `wordart.rs` (`TextWarp`) | **verified** (M1d.4): every `InlineObject` variant (pictures, charts and SmartArt with their models and draw items, shapes with freeforms, effects, connector ends, warps and dashes, groups and canvases, fields and field ranges, content-control markers, note references, bookmarks, comment and permission ranges, equations with the whole OMML node tree, ruby, positional tabs, opaque markup), `Float`, `AltText`, `WrapPolygon`, and the accessors (`is_drawing`, `frame`, `set_size`, `canvas_style`, `text_box(es)`, `is_marker`, `alt_text`/`set_alt_text`, `is_decorative`, `is_floating`, `ink`, note separators, `plain_text`) and lookups (chart, legend, SmartArt layout and colour ids, ink tools, dashes, wrap sides, ruby and positional-tab names, `corner_radius`, `ruby_offset`/`ruby_raise`, `cap_alt`, `Float::effect_extent`, wrap polygons), identical to Rust in `TestPropsMatchRust`. Not ported yet: the geometry and sanitizing helpers (`Float::spin`/`spin_pad`, `group_rects`, connector routing and sites, shape and alpha wrap outlines, dash patterns, effect bands and presets, `ChartSpec`/`SmartArtSpec` samples and sanitizing, freeform placing, `TextWarp` presets, legacy form fields), the linear-format, LaTeX and editing code of the equations, and `math_alnum`/`math_class`/`merge_runs` |
| Navigation and editing across tables and stories | `crates/doc/src/{lib,edit,fields,control}.rs`, `para.rs` (methods) | **verified** (M1e.1): paths, stories and containers (with copy-on-write through tables and parts), `para_paths`, `paths_between`, `start_of`/`end_of`, `next_para`/`prev_para`, `clamp` (an empty path is block 0, as in Rust), story text, sections (`section_index_of`, `section_mut`, `remove_section_break`, `note_options`), `add_part`, `text_box_anchor`; `insert_text`, `insert_object`, `split_paragraph`, `insert_block`, `remove_block`, `delete_range` (within a paragraph, a container, and across tables, whole tables removed, control markers kept, section breaks absorbed), `format_range`, `para_paths_in`, `format_paragraphs`, `copy_range`, `body_fragment`, `insert_fragment` (text boxes adopted), `set_story`, `restyle`, `convert_notes`, `Fragment` (`append_piece`, `from_text`, `has_equation`, `sole_equation`); range fields (`field_ranges`, `past_field_ends`, balancing); content-control walks (`control_ranges`, `control_at`, `control_text`, `typing_pos`, balancing, `walk_controls`); paragraph methods (`split_off`, `append`, `insert_object`, `delete_except`, `object_at`/`object_offsets`, `plain_text`, `final_text`, `text_without`, `deleted_ranges`, word and sentence boundaries). Random sequences of these on random documents (tables nested two deep, text boxes, notes, headers, sections, fields, controls, hostile positions) give identical results and documents in `TestEditsMatchRust` (new `edits` reference program: 20,000 documents identical; 31 of 35 planted bugs caught, the other 4 equivalent); edit.rs tests ported; fuzzed (`FuzzEdits`). Not ported yet: `shown_text`/`clipboard_text` (symbol fonts), caret stops (`prev_boundary`, `next_boundary`, `backspace_from`, which need grapheme clusters), word counts, the text box chains and reading-order walks, bookmarks and permission ranges |
| Tables (merges, row/cell props, nested) | `crates/doc/src/table.rs`, `control.rs` (types) | **verified** (M1d.3): `Table`/`Row`/`Cell` typed (nested tables, the content-control wraps of tables, rows and cells with `ContentControl`/`ControlKind`/`ControlLock`/`ListItem`), and every edit and query (`new`, insert/delete row and column, `merge_right`, `merge`, `split_cell`, `split_cell_grid`, `vertical_extent`, `cell_at_grid`, `grid_col`, `cols`, `total_width`) run in random sequences on random tables, identical to Rust in `TestPropsMatchRust`; table.rs unit tests ported; fuzzed with hostile indexes (`FuzzTableEdits`). Edits work on a `Clone` (copy on write). Not ported yet: the control walks of `control.rs` (they need inline objects) |
| Sections, columns, page setup, line numbers, doc grid | `crates/doc/src/section.rs`, `cjk_numbers.rs` | **verified** (M1d.2): every type, `SectionProps` measurements (text area, grid pitches, lines per page, characters per line, column boxes), landscape, absorb, `DocGrid::char_space_for`, and all 62 number formats (`NumFormat::format`, with the Chinese, Japanese and Korean counting systems), compared with Rust in `TestPropsMatchRust` (each format over 0–130 and every system's edges, plus random numbers); section.rs and cjk_numbers.rs unit tests ported |
| Styles, style resolution, themes | `crates/doc/src/{styles,resolve}.rs` | `styles.rs` **verified** (M1d.3): `StyleSheet` (default = the built-in set, reproduced exactly), `Style`, table style parts and regions, `PartFormat`, `PartSet::of_cell`, `get`/`find`/`gallery`/`chain`/`indents_over_list`/`table_style`/`upsert`/`add_default_gallery`/`remove`/`new_id` (with `char::is_alphanumeric` checked over every code point), `table_part`/`set_table_part`, all identical to Rust in `TestPropsMatchRust`; unit tests ported. `resolve.rs`: not started |
| Numbering and lists | `crates/doc/src/numbering.rs` | **verified** (M1d.3): `Numbering`, `AbstractNum`, `Num` (tuple overrides), `Level`, `ListKind`, `levels_for`, `ListKind::parse`, `add_list`, `restart`, `find_kind`, `shows_label`, `level`, `free_id`, and `Counters::next_label`/`values` run in random sequences, identical to Rust in `TestPropsMatchRust`; unit tests ported |
| Fields | `crates/doc/src/fields.rs` | not started |
| Comments, revisions (insert/delete/format/move) | `crates/doc/src/lib.rs` | not started |
| Footnotes, endnotes, headers/footers (parts) | `crates/doc/src/lib.rs` | not started |
| Equations (OMML tree, linear and LaTeX input) | `crates/doc/src/{math,math_*,eq}.rs` | `eq.rs` (EQ field overstrike and enclosed characters) **ported** with its test, and checked through the DOCX reader's enclosed-character fields; the rest not started |
| Charts, SmartArt, WordArt, shapes, connectors, freeforms, ink | `crates/doc/src/{chart,smart_art,wordart,connector,freeform,graphic}.rs` | not started |
| Content controls, legacy forms | `crates/doc/src/{control,legacy_form}.rs` | control walks **verified** (M1e.1); read from DOCX **verified** (M1c.4: 16,325 of 20,000 random packages identical, 24 of 24 planted bugs caught); legacy forms not started |
| Edit primitives, bidi helpers, clusters | `crates/doc/src/{edit,bidi,cluster}.rs` | not started |
| JSON document format (`.json`, `.wcraft.json`) wire-compatible with Rust | `serde` derives in `crates/doc` | **verified** for the documents tried: the Rust sample (and the sample saved as DOCX and reopened) reads and writes back **byte for byte** as `serde_json::to_vec_pretty` writes it, and Rust reads Go's output as it reads its own (`TestRustDocumentsRoundTrip`); typed parts verified as above. Unknown fields are dropped and duplicate fields refused, as serde does |

### File formats (open = `OPEN_EXTS`, save = `SAVE_EXTS` in `crates/engine/src/io.rs`)
| Format | Open | Save | Rust source | Status |
|---|---|---|---|---|
| DOCX/DOCM/DOTX/DOTM (Transitional + Strict) | ✓ | ✓ | `crates/docx` | in progress (M1c). XML tree (`crates/docx/src/xml.rs` with quick-xml 0.41) **verified**: all 6 Rust tests ported, and 400,000 generated inputs (well-formed, malformed, mutated, UTF-16, BOMs, DTDs, namespace edge cases) give the same tree or the same failure as the Rust code itself (`TestXMLMatchesRust`, 200,000 in CI; mutation-checked); fuzzed (10.5M execs). Error wording differs from quick-xml's. Kept as in Rust: `ToXML` writes names as read, so a hostile name can make re-emitted kept content invalid XML. Package access (`package.rs` with zip 3.0 and flate2 1.1.10/zlib-rs) **verified** for the ported parts (bounded zip reading, relationships, content types, `Resolve`/`Relative`, `ZipEntries`): our own zip reader (`zipread.go`) finds, opens and refuses archives as the zip crate does, because `archive/zip` disagrees on damaged ones; 30,000 generated archives (stored/deflated, ZIP64, prepended data, comments, repeated and CP437 names, NTFS/timestamp/Unicode/AES extra fields, encryption flags, bad CRCs, truncation, byte damage, tight size limits) give the same parts, relationships, content types and failures (`TestPackageMatchesRust`, also in CI; of 16 deliberate bugs, 14 are caught and 2 change no outcome); Rust tests ported; fuzzed (7.6M execs). Not ported yet: `EmbeddedManifest::parse` and the glossary fingerprint. `ZipEntries` writes with `archive/zip` at its default deflate level, so bytes differ from Rust's output; what a reader gets back is the same. **Reader** (`read/{mod,story,props}.rs`, `units.rs`, `settings_merge::has_unowned`, `drawing_color::adjust_lum`; M1c.3) **verified** for what it ports: the body (paragraphs, runs, hyperlinks, tracked insertions, deletions, moves and formatting changes, fields (simple, complex, nested, spilled, range fields for TOC/ADDIN, enclosed characters), bookmarks, comment and permission ranges, breaks, symbols, ruby, positional tabs, note references, drop caps, tables with merges, nesting and property exceptions, content controls (`sdt.rs`: block, inline, and around tables, rows and cells, every control kind, placeholders, kept extra properties; M1c.4), pictures (`w:drawing` inline and anchored: size, effect extents, every anchor, alignment and offset, all wrap kinds with wrap polygons, rotation and flips, crops, alternative text, titles, the decorative mark, hidden; media keys; M1c.5)), sections with headers and footers (chains to the depth limit), styles (aliases, level-only lists, table style regions, the default gallery), numbering (overrides, style links), settings (protection, zoom, compatibility, colour mapping, the original part kept when it holds unowned settings), theme colours and fonts, footnotes and endnotes with their separators, comments (replies, done state, durable ids, UTC dates), core and extended properties, the page background, and the parts kept verbatim (font table, unmodelled parts, with the embedded-parts manifest). `TestReadMatchesRust` reads random packages (every element and attribute above with valid and hostile values: units, signs, overflow, NaN, odd colours and theme slots, bad ids, missing and external relationships, broken XML, non-zip files) with Go and with `wordcraft_docx::read` (new `read-oracle`): 15,899 of 20,000 compared and identical, document JSON byte for byte or the same error kind; `TestReadRustWrittenMatchesRust` does the same on files Rust's writer makes of those documents and of the sample; `TestConvertDocxMatchesRust` converts the Rust CLI's `.docx` templates and script results with both CLIs. Of 77 planted bugs, 75 are caught; one is equivalent (a span `tcPr` already clamps) and one unreachable until pictures are ported (media key collisions: only the page background loads media so far). Fuzzed (`FuzzRead`, 6.5M execs; run it with `-fuzzminimizetime=0`). **Refused, not dropped** (`ErrNotPorted`, `docx.IsNotPorted`): charts, diagrams, drawing canvases, groups, shapes and text boxes, VML, OLE objects, ink, equations, run text effects (`w14:*` other than ligatures), the math settings, custom properties, bibliography sources and custom XML, glossaries, macro projects, encrypted packages, Flat OPC and Word 2003 XML. Not ported: the writer |
| Encrypted DOCX (agile) | ✓ | ✓ | `crates/docx/src/crypt.rs` | not started |
| Flat OPC XML, Word 2003 XML | ✓ | ✓ | `crates/docx/src/{flat,wordml2003*}.rs` | not started |
| DOC/DOT (Word 97–2003), XOR/RC4 encrypted | ✓ | | `crates/docbin` | not started |
| TXT (with code-page detection) | ✓ | ✓ | `crates/formats/src/txt.rs` | not started |
| Markdown | ✓ | ✓ | `crates/formats/src/markdown.rs` | not started |
| HTML | ✓ | ✓ | `crates/formats/src/html.rs` | not started |
| RTF | ✓ | ✓ | `crates/formats/src/rtf.rs` | not started |
| ODT | ✓ | ✓ | `crates/formats/src/odt.rs` | not started |
| LaTeX | ✓ | ✓ | `crates/formats/src/latex.rs` | not started |
| JSON (WordCraft model) | ✓ | ✓ | `crates/engine/src/io.rs` | in progress (M1b): open (runs repaired, a paragraph to type in) and save (save metadata stamped, written atomically with craft-libs `atomic`). Documents Rust would refresh in ways not ported yet (unlocked DATE/TIME fields) are refused |
| PDF | | ✓ | `crates/pdf` | not started |
| PNG (page render) | | ✓ | `crates/render` | not started |
| Mail-merge sources: CSV, XLSX/XLSM, ODS, XLS | ✓ | | `crates/formats/src/sheet*` (the XLS writer is a test fixture) | not started |
| WMF/EMF pictures | ✓ | | `crates/metafile` | not started |
| Embedded fonts, WOFF/WOFF2 user fonts | ✓ | | `crates/docx/src/fonts.rs`, `crates/fonts` | not started |

### Layout, rendering, output
| Feature | Rust source | Status |
|---|---|---|
| Font DB, system fonts, Word font substitution | `crates/fonts` | not started |
| Shaping, ligatures, complex scripts, OpenType MATH | `crates/fonts`, `crates/layout/src/para.rs` | not started |
| Line breaking, hyphenation, justification, tabs, kinsoku | `crates/layout/src/{para,kinsoku}.rs` | not started |
| Bidi / RTL | `crates/layout`, `crates/doc/src/bidi.rs` | not started |
| Tables (row splitting, floating) | `crates/layout/src/table.rs` | not started |
| Pagination, sections, columns, headers/footers, notes | `crates/layout/src/lib.rs` | not started |
| Floats, wrap, contour, text boxes, WordArt | `crates/layout/src/{contour,textbox,wordart}.rs` | not started |
| Math layout | `crates/layout/src/math.rs` | not started |
| Vertical text | `crates/layout/src/vertical.rs` | not started |
| Comment balloons, markup | `crates/layout/src/balloons.rs` | not started |
| Hit testing, caret placement | `crates/layout/src/hit.rs` | not started |
| Incremental relayout | `crates/layout/src/lib.rs` | not started |
| Rasterising pages and thumbnails | `crates/render` | not started |
| PDF: subsets, selectable text, links, outline, tagging | `crates/pdf` | not started |

### Engine and automation
| Feature | Rust source | Status |
|---|---|---|
| `Session`, selection, undo/redo, Repeat, macro recording | `crates/engine/src/session.rs` | in progress (M1b): `Session`, selection, undo/redo (500 steps, typing coalesced, a caret move ends the group), a failed or panicking command restores the session. Repeat and macro recording not started |
| Command registry framework (`CommandSpec`, `enabled`, `mutates`) | `crates/engine/src/lib.rs` | in progress (M1b): `CommandSpec`, `Registry`, `Session.Run`. Ported commands: `text.insert`, `format.bold`, `select.text`, `select.range`, `select.collapse`, `edit.undo`, `edit.redo`, each covering the cases its doc comment lists and refusing the rest as not ported. **Verified** on those cases: random scripts run through both CLIs print the same results and errors and save the same documents (`TestRunMatchesRust`; 3,000 scripts and 11,882 steps checked locally, 1,000 in CI; six deliberate bugs, five caught by it and the sixth by a unit test); `TestHostileParamsNeverPanic`; `FuzzCommands` |
| Restrict Editing / protection checks | `crates/engine/src/cmd/protect.rs` | not started (M1b refuses editing commands on protected documents) |
| Commands by namespace (633 total, 36 namespaces): `para` 50, `table` 48, `insert` 44, `format` 39, `view` 36, `review` 35, `edit` 31, `file` 28, `outline` 26, `references` 25, `text` 22, `equation` 20, `mailings` 19, `layout` 18, `styles` 16, `draw` 16, `developer` 16, `caret` 16, `arrange` 15, `select` 14, `picture` 14, `tools` 11, `design` 11, `shape` 9, `smartArt` 8, `fields` 8, `readAloud` 7, `document` 6, `chart` 5, `wordArt` 4, `font` 4, `canvas` 4, `list` 3, `hf` 3, `object` 1, `link` 1 | `crates/engine/src/cmd/*.rs` | not started (tracked per namespace in `docs/go-port-commands.md`, created in M4) |
| Word feature catalog and parity floor | `crates/engine/src/catalog.rs` | not started |
| Typing parity (Enter, Backspace, Tab, AutoCorrect, AutoFormat) | `crates/engine/src/tests_typing.rs`, `docs/typing-parity.md` | not started |
| Samples, templates | `crates/engine/src/sample.rs`, `cmd/templates.rs` | not started |
| Crash recovery, safe save | `crates/engine/src/{recovery,io/safe_save}.rs` | not started |
| Proofing per `ProofLang` (depth varies by language: e.g. English and Polish have spelling, grammar and hyphenation; Bulgarian has hyphenation only; Vietnamese is language detection only) | `crates/proof` | not started |
| CLI | `apps/wordcraft-cli` | in progress (M1b): `convert IN OUT`, `run --file F --cmd ID[=JSON]… --print --save OUT` and `commands` for JSON documents, with Rust's output and error format. Other subcommands and options refused as not ported |
| MCP server (headless and bridged) | `crates/mcp` | not started |
| Control channel + keys | `crates/ui-egui/src/control.rs`, `crates/control-key` | not started |
| Zotero integration | `crates/zotero` | not started |

### Front end and platforms
| Feature | Rust source | Status |
|---|---|---|
| Ribbon, keytips, mini toolbar, Backstage | `crates/ui-egui/src/{ribbon,keytips,mini_toolbar,backstage}.rs` | not started |
| Document canvas, rulers, scrolling, caret | `crates/ui-egui/src/{canvas,guides,ruler_tabs,scroll}.rs` | not started |
| Dialogs (~59) | `crates/ui-egui/src/dialogs*.rs` | not started |
| Panes (Navigation, Styles, Reviewing, Accessibility) | `crates/ui-egui/src/{panes,a11y_pane}.rs` | not started |
| Interface languages (21 catalogs) | `crates/ui-egui/src/i18n` | not started |
| Themes, dark mode, icons drawn in code | `crates/ui-egui/src/{theme,icons,desktop_theme}.rs` | not started |
| `ui.*` commands (36) | `crates/ui-egui/src/lib.rs` | not started |
| Accessibility (screen readers) | `crates/ui-egui/src/a11y.rs`, AccessKit | not started |
| Printing | `crates/ui-egui/src/printing.rs`, craft-print | not started |
| Read Aloud | `crates/engine/src/speech.rs`, `crates/ui-egui/src/read_aloud.rs` | not started |
| macOS, Windows, Linux, FreeBSD desktop | `apps/wordcraft` | not started |
| Linux builds of the Go command line (x86-64, ARM64; static, no cgo) | — (Go only) | done: every CI run builds `wordcraft-cli` for Linux, smoke-tests it and keeps it as a workflow artifact (`linux-build` job) |
| Browser (wasm), IndexedDB storage | `apps/wordcraft-web` | not started |
| Packaging (DMG, MSI, AppImage, deb/rpm, Flatpak, FreeBSD) | `packaging/`, `.github/workflows/release.yml` | not started |

## 7. Engineering rules for the Go code

The project's standards are carried over and translated into Go terms. M0 writes these into `CLAUDE.md`/`AGENTS.md`.

- **Never crash.** Go can panic implicitly (nil dereference, index out of range, nil map write, failed type
  assertion), so no linter can rule panics out entirely. Defence in layers:
  - no explicit `panic` and no `must*` helpers in non-test code (enforced by a `tools/ci` analyzer);
  - `recover()` at every boundary: `Session.Run`, I/O open/save, MCP and control-channel handlers, every
    call into go-text. A recovered panic becomes an error, and the document is restored from the undo snapshot, as Rust does;
  - input-derived numbers are hostile: bounds-checked indexing helpers, capped allocations and recursion;
  - `go test -fuzz` on every reader; `HostileParamsNeverPanic` (port of `hostile_params_never_panic`) runs every
    command with junk params.
- **Errors** are `error` values, wrapped with `%w`, with typed errors where Rust has enum variants (`DocError`, `CmdError`).
- **Snapshots and immutability.** Rust's undo relies on `Arc<Block>` sharing. In Go, blocks are shared by pointer and treated as
  immutable once in a snapshot: edits copy the block before changing it (copy-on-write helpers in `internal/doc`).
  A test-only checker hashes snapshots before and after commands to catch mutation in place.
- **Everything is a command.** The registry keeps the Rust shape (`ID, Label, Location, Shortcut, Params, Mutates, Enabled, Run`) and the same ids,
  so CLI scripts, MCP clients and macros keep working.
- **Layering** as in §2, checked by `tools/ci layers`. **wasm**: `tools/ci wasm` builds every package below L6
  (and the web app) with `GOOS=js GOARCH=wasm`.
- **Assets, clean room, attribution:** unchanged. No Microsoft (or Adobe/Avid/Autodesk) assets, ever; every asset file
  has an `ATTRIBUTION.md` row (`tools/ci assets`); OOXML, RTF and ODF from public specs only; no GPL/LGPL/AGPL code.
  Porting WordCraft's own MIT/Apache code from Rust to Go is fine: it is the same project and licence.
- **Licence:** the Go code stays `MIT OR Apache-2.0`, like the Rust code.
- **Gate before every commit** (Go side): `gofmt -l` empty, `go vet ./...`, `staticcheck ./...`, `go test ./...`,
  `tools/ci layers assets wasm`, `govulncheck ./...` (`staticcheck` from `honnef.co/go/tools` v0.8.2, `govulncheck` from `golang.org/x/vuln` v1.9.0). `cargo xtask ci` remains the gate for any commit that touches Rust.

## 8. Testing and compatibility strategy

1. **Document the Rust behaviour first.** Each component starts with a short note in its package doc: the Rust behaviour, the
   Rust tests that pin it, and the edge cases.
2. **Port the tests.** The Rust unit, round-trip and hostile-input tests are ported alongside the code. The baseline has
   3,490 `#[test]` functions plus property tests; the parity table counts how many have been ported per package.
3. **Compare with the frozen Rust app (differential tests).** `internal/rustref` (test-only) runs the baseline `wordcraft-cli`
   built from the baseline commit:
   - Model: `wordcraft-cli convert in.<ext> out.json` against Go's JSON for the same input, compared after
     normalising (key order, float formatting). This works because Rust's `Document` is fully serde-serialisable and `json`
     is a supported format.
   - Writers: a file written by Go is read by Rust (and the reverse), and the two JSON outputs must match.
   - Text and info: `wordcraft-cli text` / `info` / `inspect` output compared directly.
   - Rendering/PDF: Rust `render` PNGs against Go PNGs with a per-pixel threshold. PDF is compared as extracted text and the
     structure tree.
   - Commands: one script (`wordcraft-cli run --cmd …`) run through both engines, with the resulting JSON compared.
   These tests run when `WORDCRAFT_RUST_CLI` is set, and in CI. They are skipped elsewhere and never needed for a release build.
   Checked 2026-10-20: `cargo build --release -p wordcraft-cli` at the baseline builds here, and
   `wordcraft-cli run --template sample --cmd 'select.text={"text":"Studio"}' --cmd format.bold --save s.docx` followed by
   `wordcraft-cli convert s.docx s.json` produces the model JSON (top-level keys `body, lastSection, parts, styles,
   numbering, comments, revisions, settings, core, sources, customProps`).
4. **Fixtures.** The Rust tests build their documents in code; there are no binary fixtures in the repo. We generate a
   fixture corpus by running those Rust builders and writing DOCX/RTF/ODT/… into `testdata/` (our own output, never a
   file produced by Word). This also starts on gap #1 of `docs/gaps.md`: real-world corpus testing, from public-domain
   or self-made files only.
5. **Fuzzing:** a fuzz target per reader and for the command dispatcher; seed corpora come from the fixtures.
6. **Never-crash regression:** every crash fix lands with a test, as today.
7. **Browser:** `GOOS=js GOARCH=wasm go test` for packages below L6, with `$GOROOT/lib/wasm` on `PATH` (its `go_js_wasm_exec` runs the tests under Node).

## 9. Milestones

Each milestone is a series of small PRs, one component each. Each PR contains: the Rust behaviour note, the Go code,
the ported and differential tests, the exact commands run with their results, and known gaps. No Rust is deleted before §2's
cutover.

| # | Milestone | Contents | Exit criteria |
|---|---|---|---|
| M0 ✓ | Groundwork | `go.mod` (`go 1.26`, `toolchain go1.27.3`); `tools/ci` (fmt, vet, staticcheck, test, layers, assets, wasm, govulncheck, licences, no-panic analyzer); GitHub Actions job for Go; `CLAUDE.md`/`AGENTS.md` updated for two languages; baseline pinned by SHA; `internal/rustref` harness | `tools/ci` green on an empty module; Rust CLI builds from the baseline and runs as the reference |
| M1 | **Vertical slice** | `geom` (full); `doc` subset (Document, Paragraph, Run, a CharProps/ParaProps subset, JSON-compatible); `docx` subset reader/writer (body paragraphs and runs; other parts passed through; **saving refuses** documents with unsupported elements rather than dropping them); `engine` core (`Session`, registry, `Run` with recover + snapshot undo) with `text.insert` and `format.bold`; minimal CLI `convert`/`run` | Ported tests pass; JSON matches the Rust reference on the slice fixtures; Rust reopens a Go-saved file with identical JSON; fuzzers run for 10 min with no crash; wasm build passes |
| M2 | Complete model | All of `internal/doc` and the JSON format | JSON round-trip identical to Rust on the full fixture corpus |
| M3 | DOCX complete | Reader/writer, Strict, Flat/2003 XML, glossary, content controls, charts/SmartArt data, encryption, `internal/cfb` writer | Ported `crates/docx/tests/*` pass; differential tests on the corpus |
| M4 | Headless engine | All 633 commands, namespace by namespace; catalog; typing parity; samples; I/O dispatch; recovery; full CLI | `HostileParamsNeverPanic`; ported `engine` tests incl. `tests_typing.rs`; script comparisons against Rust |
| M5 | Agents | `mcp`, `controlkey`, `zotero` | MCP acceptance tests ported; same tool list and schemas as Rust |
| M6 | Other formats | `formats`, `docbin`, `metafile`, `proof` (+ chardet port) | Ported tests; differential tests per format |
| M7 | Fonts and layout | go-text integration, MATH parser, WOFF2, `layout` (largest milestone; split per subsystem) | Ported layout tests (9.4k lines in `tests.rs` + topic files); page breaks within tolerance against Rust on the corpus |
| M8 | Render and PDF | Rasteriser spike → `render`; own `pdf` writer | Pixel differences under threshold; PDF text, links, outline and tags checked |
| M9 | UI spike → UI | Toolkit spike (§4), then ribbon, canvas, dialogs, panes, i18n, a11y, `ui.*` commands, control channel | Spike report approved by owner; then screen-by-screen comparison with the Rust app (`ui_shot` against Go screenshots) |
| M10 | Desktop and platforms | `cmd/wordcraft`, `internal/platform` (macOS, Windows, Linux, FreeBSD), printing, file associations | Launches and passes the alpha-gate workflows on each OS |
| M11 | Browser | `cmd/wordcraft-web`, IndexedDB storage, clipboard, downloads; size budget (brotli-compressed `.wasm`) | Alpha-gate workflows in Chrome, Firefox and Safari |
| M12 | Packaging and cutover | Port `packaging/` and `release.yml`; final parity review; tag `rust-final`; delete the Rust tree | Every checklist row is `verified` or a documented, owner-accepted `gap` |

Order rationale: M1–M6 give a useful headless Go product (CLI and MCP) early, and use the Rust reference where it is
strongest. Layout, render and UI come later, after the model is proven.

## 10. Recommended first vertical slice (M1) in detail

1. Port `crates/geom` in full (743 lines) and its tests.
2. `internal/doc`: `Document`, `Block`, `Paragraph{Text, Runs, Props, Mark}`, `Run{Len, Props}`, `CharProps`
   (bold, italic, size, font, colour, style), `ParaProps` (style, alignment, spacing), and `Settings`/`StyleSheet`
   kept as raw JSON. JSON tags match serde's output for these types exactly. `Run.Len` counts UTF-8 bytes of
   `Paragraph.Text` (as Rust's does, `crates/doc/src/para.rs:38`), which matches Go string indexing; run
   boundaries are still kept on character boundaries.
3. `internal/docx`: read `word/document.xml` paragraphs and runs; pass every other part through byte for byte; write
   back. Any element outside the subset makes `Save` return an error (`ErrUnsupportedContent`) instead of losing it.
4. `internal/engine`: `Session`, `CommandSpec`, `Registry`, `Run` (enabled check, snapshot, `recover`, undo),
   `text.insert`, `format.bold`, `edit.undo`, `edit.redo`, with behaviour taken from the Rust code and tests. (Seen on
   the Rust reference: `format.bold` sets both `bold` and `boldCs`.)
5. `cmd/wordcraft-cli`: `convert in.docx out.docx|out.json` and `run --file … --cmd … --save …`.
6. Tests: ported unit tests; round-trips; differential checks against `wordcraft-cli` built from the baseline commit; fuzz targets for
   the reader and for `Run` with junk params; wasm build.

## 11. Known risks and open items

| Risk | Impact | Mitigation |
|---|---|---|
| Layout parity depends on shaping matching HarfRust | Page breaks differ → layout tests fail | Port semantic layout tests first; position tolerances; compare shaping per run with Rust on the corpus; report go-text bugs upstream |
| go-text and Gio are pre-1.0 (breaking changes in minor versions) | Upgrade churn | Pin versions; one reviewed upgrade PR at a time; wrap them behind our own small interfaces |
| Gio's WebAssembly support is marked experimental | Browser build may be weak | The UI spike tests the browser first; Fyne is the fallback; kept as an explicit spike exit criterion |
| Go wasm binary size and speed | Slow first load | Brotli-compressed serving, lazy font loading, size budget tracked in CI. TinyGo is not planned for now: its reflection support is incomplete and `encoding/json` is used throughout. That can be revisited with measurements |
| Implicit panics in Go | Crash = lost work | §7 defence in layers; fuzzing; recover at boundaries |
| Copy-on-write discipline isn't compiler-checked | Undo corruption | Snapshot hashing checker in tests; small, reviewed mutation API in `internal/doc` |
| No Go WOFF2 decoder, no OpenType MATH table parser | User web fonts and equations | Write them (both are public specs); Brotli via `github.com/andybalholm/brotli` v1.2.6 (MIT, released 2026-09) |
| `craft-libs` (craft-pdf, craft-ui, craft-print) | Resolved 2026-10-20: the owner forked it to `computernut43/craft-libs`; licence `MIT OR Apache-2.0` (krilla and hayro too) | Ported there in Go (its `MIGRATION.md`); WordCraft pins it by commit |
| A non-Go `vendor/` directory in craft-libs (Rust krilla/hayro sources) makes the go command assume Go vendoring there | Plain `go build` fails inside craft-libs (verified) | Its `tools/ci` sets `GOFLAGS=-mod=mod`; modules that require craft-libs are unaffected (verified: WordCraft builds against it) |
| 633 commands + 63k-line UI is most of the work | Schedule | Port by namespace with the catalog as the progress meter; UI is a rewrite against the same command ids |
| The frozen Rust app may have bugs | The reference can be wrong | Differences are reviewed, not auto-accepted; a confirmed Rust bug is fixed in Go and noted in the parity row |
| Docs drift (architecture doc said 85.6k lines / 433 commands; actual 272k lines incl. tests / 633 commands) | Wrong estimates | Numbers in this plan are measured from the source at the baseline; re-measure at each milestone |

## Revision history

| Date | Change | Summary |
|---|---|---|
| 2026-10-20 | minor | M1c.5: pictures read from DOCX, verified against `wordcraft_docx::read` (23 of 24 planted bugs caught, the other unreachable until ink); `geom` now exact against Rust with craft-libs `rustcompat.Sin32`/`Cos32`; `doc.Float` gains `Spin`, `SetSpin`, `SpinPad` |
| 2026-10-20 | minor | M1c.4: content controls read from DOCX (`sdt.rs`), verified against `wordcraft_docx::read`; no longer refused |
| 2026-10-20 | minor | M1c.3: the DOCX reader's core ported and verified against `wordcraft_docx::read` (new `read-oracle`, with a `write-docx` helper); features not ported yet are refused with `ErrNotPorted` rather than dropped; the engine and CLI open `.docx`/`.docm`/`.dotx`/`.dotm`; `eq.rs` ported; `docx.Error` kinds now mirror `DocxError` |
| 2026-10-20 | minor | M1e.1: navigation and editing across tables and stories, range fields and content-control walks ported and verified (`edits` reference program); the engine clamps and opens documents with tables as Rust does |
| 2026-10-20 | minor | M1d.4: inline objects typed and verified (every variant with charts, SmartArt, shapes, groups, equations' node tree), the paragraph's section typed; `Raw` removed; float helpers now from craft-libs `rustcompat` |
| 2026-10-20 | minor | M1d.3: styles (with the built-in sheet), numbering and list counters, tables and their edits, and the content-control types typed and verified against `crates/doc` (109,968 inputs identical; 17 planted bugs caught); tables, styles and numbering are no longer `Raw` |
| 2026-10-20 | minor | M1d.2: sections (with all number formats and the CJK counting systems), settings, parts, comments, revisions, sources, custom properties and glossary typed and verified against `crates/doc` |
| 2026-10-20 | minor | M1d.1: `internal/doc` typed with craft-libs `serdejson` (all of `props.rs`, run text effects, core properties, paragraphs, runs, blocks, documents); `Extra` replaced by `Raw` for the parts not typed yet; JSON output byte for byte as Rust's; verified by a new props reference program; doc oracle pinned to the baseline's serde and serde_json |
| 2026-10-20 | minor | Linux builds of `wordcraft-cli` in CI |
| 2026-10-20 | minor | M1c.2: `internal/docx` package access with a zip reader matching the zip crate, verified against `package.rs` |
| 2026-10-20 | minor | M1c.1: `internal/docx` XML tree, verified against `crates/docx/src/xml.rs` |
| 2026-10-20 | minor | M1b: `internal/engine` core (session, undo, registry, seven commands, JSON open/save) and `cmd/wordcraft-cli`, verified against the Rust CLI by random scripts |
| 2026-10-20 | minor | M1a: `internal/doc` model with lossless JSON and paragraph edits, verified against Rust |
| 2026-10-20 | minor | M1: `internal/geom` ported and verified against Rust |
| 2026-10-20 | minor | M0 landed: Go module, `tools/ci` (shared gates from craft-libs + layers and assets ported from xtask), `internal/rustref` (checked against the real Rust CLI), Go workflow, agent instructions; craft-libs brought into scope; layer table corrected to the Rust gate's |
| 2026-10-20 | major | First version: audit findings, owner decisions, library checks, checklist and milestones |
