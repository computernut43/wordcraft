package docx

// Word bibliography sources: a b:Sources custom XML part of a document (Rust
// crates/docx/src/bibliography.rs parse, write::from_source_type; read/mod.rs read_bibliography),
// and the custom XML stores kept for saving (read/embed.rs keep_custom_xml).

import (
	"strings"

	"github.com/computernut43/craft-libs/rustcompat"

	"github.com/computernut43/wordcraft/internal/doc"
)

const (
	// maxSources is the most bibliography sources kept from one file.
	maxSources = 10_000
	// maxStyleName is the characters kept of a bibliography's citation style name.
	maxStyleName = 64
	// customXMLRoots lists the custom XML roots kept by the package (P) or main document (D):
	// owner, type and part per line.
	customXMLRoots = "wordcraft:customXml.roots"
)

// bibSeen are the bibliography sources already read: tags, and (title, author) of untagged ones.
type bibSeen struct {
	tags     map[string]bool
	untagged map[[2]string]bool
}

// parseSources reads the fresh sources and style of a parsed b:Sources root element: seen carries
// the tags and untagged (title, author) keys already taken, so repeated parts collapse into one
// source, and at most room fresh sources are built.
func parseSources(root *El, seen *bibSeen, room int) ([]doc.Source, *string) {
	var style *string
	for _, a := range root.Attrs {
		local := a.Key
		if i := strings.LastIndexByte(local, ':'); i >= 0 {
			local = local[i+1:]
		}
		if local != "StyleName" {
			continue
		}
		if name := rustcompat.Trim(a.Val); name != "" {
			style = ptr(takeChars(name, maxStyleName))
		}
		break
	}
	childText := func(el *El, local string) string {
		for _, e := range el.Els() {
			if e.Local() == local {
				return e.Text()
			}
		}
		return ""
	}
	firstLocal := func(el *El, local string) *El {
		for _, e := range el.Els() {
			if e.Local() == local {
				return e
			}
		}
		return nil
	}
	persons := func(parent *El) string {
		var people []*El
		for _, n := range parent.Els() {
			if n.Local() == "NameList" {
				for _, p := range n.Els() {
					if p.Local() == "Person" {
						people = append(people, p)
					}
				}
			}
		}
		if len(people) == 0 {
			for _, p := range parent.Els() {
				if p.Local() == "Person" {
					people = append(people, p)
				}
			}
		}
		var names []string
		for _, p := range people {
			last, first, middle := childText(p, "Last"), childText(p, "First"), childText(p, "Middle")
			name := last
			switch {
			case first != "" && middle != "":
				name = last + ", " + first + " " + middle
			case first != "":
				name = last + ", " + first
			}
			if name != "" {
				names = append(names, name)
			}
		}
		return strings.Join(names, "; ")
	}
	var out []doc.Source
	for _, src := range root.Els() {
		if src.Local() != "Source" {
			continue
		}
		if len(out) >= room {
			break
		}
		tag := childText(src, "Tag")
		kind := fromSourceType(childText(src, "SourceType"))
		var author, editor string
		if a := firstLocal(src, "Author"); a != nil {
			if inner := firstLocal(a, "Author"); inner != nil {
				author = persons(inner)
			}
			if author == "" {
				author = persons(a)
			}
			if ed := firstLocal(a, "Editor"); ed != nil {
				editor = persons(ed)
			}
		}
		if editor == "" {
			if ed := firstLocal(src, "Editor"); ed != nil {
				editor = persons(ed)
			}
		}
		title := childText(src, "Title")
		accessed := childText(src, "Accessed")
		if accessed == "" {
			y, m, d := childText(src, "YearAccessed"), childText(src, "MonthAccessed"), childText(src, "DayAccessed")
			switch {
			case y != "" && m != "" && d != "":
				accessed = y + "-" + m + "-" + d
			case y != "" && m != "":
				accessed = y + "-" + m
			default:
				accessed = y
			}
		}
		doi := childText(src, "DOI")
		if doi == "" {
			doi = childText(src, "StandardNumber")
		}
		// Keep sources with some content, once each (by tag, or title and author untagged).
		if tag == "" && title == "" && author == "" && editor == "" {
			continue
		}
		if tag == "" {
			who := author
			if who == "" {
				who = editor
			}
			key := [2]string{title, who}
			if seen.untagged[key] {
				continue
			}
			seen.untagged[key] = true
		} else {
			if seen.tags[tag] {
				continue
			}
			seen.tags[tag] = true
		}
		out = append(out, doc.Source{
			Tag: tag, Kind: kind, Author: author, Title: title, Year: childText(src, "Year"), City: childText(src, "City"),
			Publisher: childText(src, "Publisher"), Journal: childText(src, "JournalName"), Volume: childText(src, "Volume"),
			Pages: childText(src, "Pages"), URL: childText(src, "URL"), Editor: editor, Edition: childText(src, "Edition"),
			Issue: childText(src, "Issue"), Month: childText(src, "Month"), Day: childText(src, "Day"), Accessed: accessed, DOI: doi,
		})
	}
	return out, style
}

// fromSourceType is the model's source kind for a b:SourceType.
func fromSourceType(st string) string {
	switch t := rustcompat.Trim(st); t {
	case "Book", "book", "":
		return "book"
	case "BookSection", "booksection":
		return "bookSection"
	case "JournalArticle", "journalarticle", "article":
		return "article"
	case "ArticleInAPeriodical", "articleinaperiodical":
		return "periodical"
	case "InternetSite", "internetsite", "website", "DocumentFromInternetSite", "documentfrominternetsite":
		return "website"
	case "ElectronicSource", "electronicsource":
		return "electronicsource"
	case "Report", "report":
		return "report"
	case "ConferenceProceedings", "conferenceproceedings":
		return "conference"
	case "Case", "case":
		return "case"
	case "SoundRecording", "soundrecording":
		return "soundrecording"
	case "Film", "film":
		return "film"
	case "Interview", "interview":
		return "interview"
	case "Patent", "patent":
		return "patent"
	case "Performance", "performance":
		return "performance"
	case "Standard", "standard":
		return "standard"
	case "Thesis", "thesis":
		return "thesis"
	case "Misc", "misc":
		return "other"
	default:
		return toASCIILower(t)
	}
}

// readBibliography reads the sources of a b:Sources custom XML part (other custom XML is left
// alone); at most maxSources in all, and the document's citation style from StyleName.
func (r *reader) readBibliography(path string, seen *bibSeen) {
	root, err := r.pkg.XML(path)
	if err != nil || root == nil || root.Local() != "Sources" {
		return
	}
	sources, style := parseSources(root, seen, max(maxSources-len(r.doc.Sources), 0))
	if r.doc.Settings.BibStyle == nil {
		r.doc.Settings.BibStyle = style
	}
	r.doc.Sources = append(r.doc.Sources, sources...)
}

// keepCustomXML keeps the custom XML stores reached from the main document or package, without
// evaluating bindings. A bibliography already loaded into the source model belongs to its writer.
func (r *reader) keepCustomXML(pkgRels, docRels Rels) {
	var roots strings.Builder
	for _, owner := range []struct {
		name string
		rels Rels
	}{{"P", pkgRels}, {"D", docRels}} {
		n := 0
		for _, rel := range owner.rels {
			if rel.External || !RelIs(rel.Kind, relCustomXMLRT) {
				continue
			}
			if n >= maxEmbeddedRels {
				break
			}
			n++
			if !manifestFieldOK(rel.Kind) || !manifestFieldOK(rel.Target) {
				continue
			}
			if len(r.doc.Sources) > 0 {
				if x, err := r.pkg.XML(rel.Target); err == nil && x != nil && x.Local() == "Sources" {
					continue
				}
			}
			// Native ink shares the custom XML relationship type, but its part belongs to the
			// drawing that refers to it: kept with it, or dropped with it (no orphan part).
			if ct, ok := r.types().Of(rel.Target); ok && ct == "application/inkml+xml" {
				continue
			}
			if r.keepPartTree(rel.Target) {
				roots.WriteString(owner.name + "\t" + rel.Kind + "\t" + rel.Target + "\n")
			}
		}
	}
	if roots.Len() > 0 {
		r.doc.Passthrough = ensureMap(r.doc.Passthrough)
		r.doc.Passthrough[customXMLRoots] = doc.Vec[uint8](roots.String())
	}
}
