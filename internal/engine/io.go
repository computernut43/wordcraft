package engine

// Opening and saving documents (Rust crates/engine/src/io.rs and file.save). The WordCraft JSON
// format (.json, .wcraft.json) is ported, and opening Word packages (.docx, .docm, .dotx, .dotm)
// for what internal/docx reads; other formats aren't yet.

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/computernut43/craft-libs/atomic"

	"github.com/computernut43/wordcraft/internal/doc"
	"github.com/computernut43/wordcraft/internal/docx"
)

// maxOpenBytes caps how much of a file is read (a document, not a disk image).
const maxOpenBytes = 1 << 30

func isJSON(path string) bool {
	return extOf(path) == "json"
}

// extOf is a file name's extension, lower case, without the dot (Rust ext_of).
func extOf(name string) string {
	file := name
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		file = name[i+1:]
	}
	i := strings.LastIndexByte(file, '.')
	if i < 0 {
		return ""
	}
	return strings.ToLower(file[i+1:])
}

// File signatures (Rust ZIP_SIG, OLE_SIG).
var (
	zipSig = []byte("PK\x03\x04")
	oleSig = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
)

// OpenBytes parses a document by its file name's extension, as Rust's open_bytes does: runs
// repaired, a paragraph to type in.
func OpenBytes(name string, data []byte) (*doc.Document, error) {
	var d *doc.Document
	switch ext := extOf(name); ext {
	case "json":
		var err error
		if d, err = doc.Unmarshal(data); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		d.NormalizeRuns()
	case "docx", "docm", "dotx", "dotm":
		// Rust reads an encrypted package (a compound file) with its password, and a file whose
		// signature says it is another format (Word 97-2003, RTF, Word XML, a web page) by its
		// content. Neither is ported.
		if bytes.HasPrefix(data, oleSig) {
			return nil, notPorted("encrypted or Word 97-2003 documents")
		}
		if !bytes.HasPrefix(data, zipSig) && otherSignature(data) {
			return nil, notPorted("opening .%s files that hold another format", ext)
		}
		var err error
		if d, err = docx.Read(data); err != nil {
			if docx.IsNotPorted(err) {
				return nil, notPorted("%s", strings.TrimPrefix(err.Error(), "not ported to Go yet: "))
			}
			return nil, err
		}
	default:
		return nil, notPorted("opening %s files", strings.ToLower(filepath.Ext(name)))
	}
	d.EnsureNonempty()
	if d.NeedsDateRefresh() {
		return nil, notPorted("documents with DATE or TIME fields (opening refreshes them)")
	}
	return d, nil
}

// otherSignature reports bytes that may be RTF, XML or HTML (what Rust's signature_contradicts
// tells apart; any of them is refused rather than read as a package).
func otherSignature(data []byte) bool {
	b := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	b = bytes.TrimLeft(b, " \t\r\n\f")
	return bytes.HasPrefix(b, []byte("{\\rtf")) || bytes.HasPrefix(b, []byte("<"))
}

// Open reads and parses a document file.
func Open(path string) (*doc.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxOpenBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxOpenBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, maxOpenBytes)
	}
	return OpenBytes(path, data)
}

// SaveBytes writes the document in the format path's extension names (Rust save_bytes).
func SaveBytes(path string, d *doc.Document) ([]byte, error) {
	if !isJSON(path) {
		return nil, notPorted("saving %s files", strings.ToLower(filepath.Ext(path)))
	}
	return d.Marshal()
}

// Save stamps the document's save metadata, as Word does on every save (Rust Session::stamp_save),
// and writes it to path atomically: a failed save leaves any existing file as it was.
func (s *Session) Save(path string) error {
	if !isJSON(path) {
		return notPorted("saving %s files", strings.ToLower(filepath.Ext(path)))
	}
	stamped := s.Doc.Clone()
	if err := stampSave(stamped, s.Author, time.Now()); err != nil {
		return err
	}
	data, err := SaveBytes(path, stamped)
	if err != nil {
		return err
	}
	if err := atomic.Write(path, data); err != nil {
		return err
	}
	s.Doc, s.Dirty = stamped, false
	return nil
}

// stampSave sets what Rust's stamp_save sets: the zoom the document is shown at (a headless
// session shows it at 100%, saved as no zoom), the modified time, the creation time when there is
// none, who modified it last and the revision count.
func stampSave(d *doc.Document, author string, now time.Time) error {
	d.Settings.Zoom = nil
	c := &d.Core
	c.Modified = now.UTC().Format("2006-01-02T15:04:05Z")
	if c.Created == "" {
		c.Created = c.Modified
	}
	c.LastModifiedBy = author
	if c.Revision < math.MaxUint32 {
		c.Revision++
	}
	return nil
}
