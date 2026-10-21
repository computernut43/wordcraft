package docx

// The glossary document (ECMA-376 Part 1 §17.12): the document's own building blocks (Quick
// Parts, AutoText…), read into Document.Glossary (Rust crates/docx/src/read/glossary.rs).
//
// The glossary part and everything it reaches are also kept verbatim (keepUnmodelled), with a
// fingerprint of the entries as read (glossaryRead): a save writes the kept part while the entries
// are unchanged, so whatever the model doesn't hold survives, and writes the glossary from the
// model once they change.
//
// The fingerprint deliberately differs from Rust's. Rust hashes the entries' derived Debug text,
// which includes each paragraph's rev, a process-wide counter: the same file read twice in one
// process gets two fingerprints, and none survives a JSON round trip. No port can reproduce that
// value, so this one hashes the entries' JSON instead (FNV-1a, 64 bits): equal entries give an
// equal fingerprint, and a mismatch (a document fingerprinted by Rust, say) only means the
// glossary is written from the model, as Rust does then.

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/computernut43/wordcraft/internal/doc"
)

const (
	// glossaryRead keeps the fingerprint of the glossary as read.
	glossaryRead = "wordcraft:glossary.read"
	// glossaryContentType is the glossary part's content type.
	glossaryContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.glossary+xml"
	// maxGlossaryEntries is the most building blocks kept.
	maxGlossaryEntries = 10_000
	// maxGlossaryField is the longest name, gallery, category, description, guid or style kept
	// (characters).
	maxGlossaryField = 1024
)

var (
	glossaryTypes     = []string{"none", "normal", "autoExp", "toolbar", "speller", "formFld", "bbPlcHdr"}
	glossaryBehaviors = []string{"content", "p", "pg"}
)

// glossaryListed is the known values of parent's tag children, each once, in file order.
func glossaryListed(parent *El, tag string, known []string) doc.Vec[string] {
	var out doc.Vec[string]
	if parent == nil {
		return out
	}
	for _, e := range parent.Children(tag) {
		v, ok := e.Attr("w:val")
		if ok && slices.Contains(known, v) && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// glossaryFingerprint is the fingerprint of building blocks: FNV-1a of their JSON.
func glossaryFingerprint(g doc.Vec[doc.BuildingBlock]) string {
	if g == nil {
		g = doc.Vec[doc.BuildingBlock]{}
	}
	b, err := json.Marshal(g)
	if err != nil {
		b = nil
	}
	h := uint64(0xcbf2_9ce4_8422_2325)
	for _, c := range b {
		h = (h ^ uint64(c)) * 0x0100_0000_01b3
	}
	return fmt.Sprintf("%016x", h)
}

// readGlossary reads the glossary the main document relates to (docRels: its relationships). The
// first internal glossary relationship counts; one pointing at the main document, a part that
// isn't a glossary, or a missing one gives no entries. Entries without a name or a body are
// skipped.
func (r *reader) readGlossary(docRels Rels) {
	var rel *Rel
	for i := range docRels {
		if !docRels[i].External && RelIs(docRels[i].Kind, RelGlossary) {
			rel = &docRels[i]
			break
		}
	}
	if rel == nil {
		return
	}
	entries := doc.Vec[doc.BuildingBlock]{}
	if !asciiEqualFold(rel.Target, r.main) {
		ct, hasCT := r.types().Of(rel.Target)
		root, err := r.pkg.XML(rel.Target)
		// Without content types (a converted package) the root element alone decides.
		if err == nil && root != nil && root.Name == "w:glossaryDocument" && (!hasCT || ct == glossaryContentType) {
			entries = r.docParts(root, r.pkg.Rels(rel.Target))
		}
	}
	r.doc.Glossary = entries
	r.doc.Passthrough = ensureMap(r.doc.Passthrough)
	r.doc.Passthrough[glossaryRead] = doc.Vec[uint8](glossaryFingerprint(entries))
}

// docParts reads the building blocks of a w:glossaryDocument, with the glossary's relationships.
func (r *reader) docParts(root *El, rels Rels) doc.Vec[doc.BuildingBlock] {
	// The glossary's notes, comments, bookmarks, permissions and moves are its own: what the main
	// document's ids mean doesn't apply to it, and its ids mustn't leak into the body's.
	footnotes, endnotes, comments, ended, bookmarks, perms, moves := r.footnotes, r.endnotes, r.commentMap, r.commentsEnded, r.bookmarks, r.perms, r.moves
	r.footnotes, r.endnotes, r.commentMap, r.commentsEnded = map[int64]uint32{}, map[int64]uint32{}, map[string]uint32{}, map[uint32]bool{}
	r.bookmarks, r.perms, r.moves = map[string]string{}, map[string]uint32{}, moveCtx{seen: map[string]uint8{}}
	out := doc.Vec[doc.BuildingBlock]{}
	if parts := root.Child("w:docParts"); parts != nil {
		for _, part := range parts.Children("w:docPart") {
			if len(out) >= maxGlossaryEntries {
				break
			}
			pr := part.Child("w:docPartPr")
			child := func(e *El, name string) *El {
				if e == nil {
					return nil
				}
				return e.Child(name)
			}
			val := func(e *El) string {
				if e == nil {
					return ""
				}
				return takeChars(attr(e, "w:val"), maxGlossaryField)
			}
			name := val(child(pr, "w:name"))
			body := part.Child("w:docPartBody")
			if body == nil || name == "" {
				continue
			}
			category := child(pr, "w:category")
			sc := &storyCtx{}
			blocks := doc.Vec[doc.Block]{}
			r.readBlocks(sc, body, rels, &blocks, 0)
			r.flushPending(sc, &blocks)
			if len(blocks) == 0 {
				continue
			}
			out = append(out, doc.BuildingBlock{
				Name:        name,
				Gallery:     val(child(category, "w:gallery")),
				Category:    val(child(category, "w:name")),
				Description: val(child(pr, "w:description")),
				GUID:        val(child(pr, "w:guid")),
				Style:       val(child(pr, "w:style")),
				Types:       glossaryListed(child(pr, "w:types"), "w:type", glossaryTypes),
				Behaviors:   glossaryListed(child(pr, "w:behaviors"), "w:behavior", glossaryBehaviors),
				Blocks:      blocks,
			})
		}
	}
	r.footnotes, r.endnotes, r.commentMap, r.commentsEnded, r.bookmarks, r.perms, r.moves = footnotes, endnotes, comments, ended, bookmarks, perms, moves
	return out
}
