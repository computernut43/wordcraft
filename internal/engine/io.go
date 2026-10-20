package engine

// Opening and saving documents (Rust crates/engine/src/io.rs and file.save). The WordCraft JSON
// format (.json, .wcraft.json) is ported; other formats aren't yet.

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/computernut43/craft-libs/atomic"

	"github.com/computernut43/wordcraft/internal/doc"
)

// maxOpenBytes caps how much of a file is read (a document, not a disk image).
const maxOpenBytes = 1 << 30

func isJSON(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".json"
}

// OpenBytes parses a document by its file name's extension, as Rust's open_bytes does: runs
// repaired, a paragraph to type in.
func OpenBytes(name string, data []byte) (*doc.Document, error) {
	if !isJSON(name) {
		return nil, notPorted("opening %s files", strings.ToLower(filepath.Ext(name)))
	}
	d, err := doc.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	d.NormalizeRuns()
	if !d.EnsureNonempty() {
		return nil, notPorted("documents with empty table cells")
	}
	if d.NeedsDateRefresh() {
		return nil, notPorted("documents with DATE or TIME fields (opening refreshes them)")
	}
	return d, nil
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
