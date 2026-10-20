package docx

// A zip reader that opens, and refuses, the same archives as the zip crate 3.0 the Rust app
// reads packages with (default features off, "deflate" on), and reads entries as it does.
// archive/zip is stricter in some places (entry counts must match the end record) and more
// lenient in others (multi-disk fields, local header offsets past the central directory, extra
// fields), so a damaged file could open in one app and not the other. Only reading is here;
// ZipEntries writes with archive/zip.
//
// The archive is found as the crate finds it: end-of-central-directory records tried from the
// end of the file back, each checked for a central directory (possibly ZIP64, possibly after
// prepended data) that parses completely; the first that does is the archive.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math"
	"unicode/utf8"
)

var (
	sigLocal   = []byte{0x50, 0x4B, 0x03, 0x04}
	sigCentral = []byte{0x50, 0x4B, 0x01, 0x02}
	sigEnd     = []byte{0x50, 0x4B, 0x05, 0x06}
	sigEnd64   = []byte{0x50, 0x4B, 0x06, 0x06}
	sigLoc64   = []byte{0x50, 0x4B, 0x06, 0x07}
)

const (
	zipMethodStore   = 0
	zipMethodDeflate = 8
	zipMethodAES     = 99
	centralEntrySize = 46
)

// errZipIO marks a read past the data (the crate's ZipError::Io), which some steps ignore.
var errZipIO = errors.New("zip: unexpected end of data")

var errZipInvalid = errors.New("zip: invalid archive")

// zipEntry is one central directory entry, as the crate keeps it.
type zipEntry struct {
	name        string
	flags       uint16
	method      uint16
	crc         uint32
	csize       uint64
	usize       uint64
	headerStart uint64
	aes         bool
}

func (e *zipEntry) encrypted() bool { return e.flags&1 != 0 }

// le reads little-endian fields from a byte slice, failing (errZipIO) past its end.
type le struct {
	b   []byte
	pos int
	err error
}

func (r *le) take(n int) []byte {
	if r.err != nil || n < 0 || r.pos+n > len(r.b) || r.pos+n < r.pos {
		r.err = errZipIO
		return nil
	}
	s := r.b[r.pos : r.pos+n]
	r.pos += n
	return s
}

func (r *le) u8() uint8 {
	if s := r.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (r *le) u16() uint16 {
	if s := r.take(2); s != nil {
		return binary.LittleEndian.Uint16(s)
	}
	return 0
}

func (r *le) u32() uint32 {
	if s := r.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}

func (r *le) u64() uint64 {
	if s := r.take(8); s != nil {
		return binary.LittleEndian.Uint64(s)
	}
	return 0
}

// at is a reader positioned at off, or one already failed when off is past the data.
func at(b []byte, off uint64) *le {
	if off > uint64(len(b)) {
		return &le{b: b, pos: len(b), err: errZipIO}
	}
	return &le{b: b, pos: int(off)}
}

// findBackwards lists the positions of sig in b[start:end], last first.
func findBackwards(b []byte, sig []byte, start, end uint64) []uint64 {
	end = min(end, uint64(len(b)))
	if start >= end {
		return nil
	}
	var out []uint64
	hay := b[start:end]
	for {
		i := bytes.LastIndex(hay, sig)
		if i < 0 {
			return out
		}
		out = append(out, start+uint64(i))
		hay = hay[:i]
	}
}

// optimisticForward is the crate's OptimisticMagicFinder going forward: first the guess
// itself, then (unless the guess was mandatory) every match in b[start:end] in order.
func optimisticForward(b []byte, sig []byte, start, end, guess uint64, yield func(uint64) bool) {
	end = min(end, uint64(len(b)))
	if guess <= math.MaxUint64-uint64(len(sig)) && guess+uint64(len(sig)) <= end && bytes.Equal(b[guess:guess+uint64(len(sig))], sig) {
		if !yield(guess) {
			return
		}
	}
	if start >= end {
		return
	}
	for i := start; i < end; {
		j := bytes.Index(b[i:end], sig)
		if j < 0 {
			return
		}
		if !yield(i + uint64(j)) {
			return
		}
		i += uint64(j) + uint64(len(sig))
	}
}

// endRecord is what the crate takes from the end records to read the central directory.
type endRecord struct {
	position      uint64 // of the 32-bit end record
	archiveOffset uint64
	zip64         bool
	// 32-bit record
	disk, cdDisk, filesThisDisk, files uint16
	cdOffset                           uint32
	// ZIP64 record
	z64VersionMadeBy, z64VersionNeeded uint16
	z64Disk, z64CDDisk                 uint32
	z64FilesThisDisk, z64Files         uint64
	z64CDOffset                        uint64
}

// findCentralDirectory is the crate's find_central_directory over b[:endExclusive].
func findCentralDirectory(b []byte, endExclusive uint64) (endRecord, error) {
	fileLen := uint64(len(b))
	var parsingErr error
	for _, eocd := range findBackwards(b, sigEnd, 0, endExclusive) {
		r := at(b, eocd)
		r.take(4)
		e := endRecord{position: eocd}
		e.disk, e.cdDisk, e.filesThisDisk, e.files = r.u16(), r.u16(), r.u16(), r.u16()
		r.u32() // central directory size
		e.cdOffset = r.u32()
		commentLen := r.u16()
		if r.err != nil {
			if parsingErr == nil {
				parsingErr = r.err
			}
			continue
		}
		if r.take(int(commentLen)); r.err != nil {
			if parsingErr == nil {
				parsingErr = errZipInvalid
			}
			continue
		}
		if uint64(commentLen)+eocd+22 > fileLen {
			parsingErr = errZipInvalid
			continue
		}
		var loc64 *le
		loc64Off := uint64(0)
		if e.files == math.MaxUint16 || e.cdOffset == math.MaxUint32 {
			if eocd >= 20 {
				loc64Off = eocd - 20
				lr := at(b, loc64Off)
				if bytes.Equal(lr.take(4), sigLoc64) && lr.err == nil {
					loc64 = lr
				}
			}
		}
		if loc64 != nil {
			// The crate reads the whole locator before using it.
			if lr := at(b, loc64Off+4); lr.take(16) == nil {
				loc64 = nil
			}
		}
		if loc64 == nil {
			rel := uint64(e.cdOffset)
			if e.files == 0 {
				e.archiveOffset = eocd - min(rel, eocd)
				return e, nil
			}
			if rel >= eocd {
				parsingErr = errZipInvalid
				continue
			}
			found := false
			optimisticForward(b, sigCentral, rel, eocd, rel, func(cd uint64) bool {
				e.archiveOffset = cd - rel
				found = true
				return false
			})
			if found {
				return e, nil
			}
			parsingErr = errZipInvalid
			continue
		}
		locCDDisk := loc64.u32()
		locEnd64 := loc64.u64()
		locDisks := loc64.u32()
		if loc64.err != nil {
			// The locator was parsed whole by the crate before it was used.
			parsingErr = errZipInvalid
			continue
		}
		if locEnd64 >= loc64Off {
			parsingErr = errZipInvalid
			continue
		}
		if locDisks > 1 {
			parsingErr = errZipInvalid
			continue
		}
		var localErr error
		ok := false
		optimisticForward(b, sigEnd64, locEnd64, loc64Off, locEnd64, func(p uint64) bool {
			archiveOffset := p - locEnd64
			expected := loc64Off - min(p, loc64Off)
			r := at(b, p)
			r.take(4)
			recordSize := r.u64()
			madeBy, needed := r.u16(), r.u16()
			disk, cdDisk := r.u32(), r.u32()
			thisDisk, files := r.u64(), r.u64()
			r.u64() // central directory size
			cdOffset := r.u64()
			switch {
			case r.err != nil:
				localErr = r.err
				return true
			case recordSize < 44:
				localErr = errZipInvalid
				return true
			case recordSize > math.MaxUint64-12 || recordSize+12 > expected:
				localErr = errZipInvalid
				return true
			}
			if r.take(int(min(recordSize-44, math.MaxInt32))); r.err != nil {
				localErr = r.err
				return true
			}
			if cdDisk != locCDDisk || recordSize+12 != expected {
				localErr = errZipInvalid
				return true
			}
			if p < satAdd64(satMul64(files, centralEntrySize), cdOffset) {
				localErr = errZipInvalid
				return true
			}
			e.zip64 = true
			e.archiveOffset = archiveOffset
			e.z64VersionMadeBy, e.z64VersionNeeded = madeBy, needed
			e.z64Disk, e.z64CDDisk = disk, cdDisk
			e.z64FilesThisDisk, e.z64Files, e.z64CDOffset = thisDisk, files, cdOffset
			ok = true
			return false
		})
		if ok {
			return e, nil
		}
		if localErr != nil {
			parsingErr = localErr
		} else {
			parsingErr = errZipInvalid
		}
	}
	if parsingErr != nil {
		return endRecord{}, parsingErr
	}
	return endRecord{}, errZipInvalid
}

func satAdd64(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

func satMul64(a, b uint64) uint64 {
	if a != 0 && b > math.MaxUint64/a {
		return math.MaxUint64
	}
	return a * b
}

// openZip reads the central directory as the zip crate's ZipArchive::new does, and returns its
// entries with one per name (a repeated name keeps its first place and takes the later entry).
func openZip(b []byte) ([]zipEntry, error) {
	endExclusive := uint64(len(b))
	for {
		e, err := findCentralDirectory(b, endExclusive)
		if err != nil {
			return nil, err
		}
		entries, err := readCentralDirectory(b, e)
		if err != nil {
			// The next candidate end record starts before this one.
			endExclusive = e.position
			continue
		}
		var out []zipEntry
		index := map[string]int{}
		for _, ent := range entries {
			if i, ok := index[ent.name]; ok {
				out[i] = ent
				continue
			}
			index[ent.name] = len(out)
			out = append(out, ent)
		}
		return out, nil
	}
}

func readCentralDirectory(b []byte, e endRecord) ([]zipEntry, error) {
	var relCD, files uint64
	var disk, cdDisk uint32
	if e.zip64 {
		if e.z64FilesThisDisk > e.z64Files || e.z64VersionNeeded > e.z64VersionMadeBy {
			return nil, errZipInvalid
		}
		relCD, files, disk, cdDisk = e.z64CDOffset, e.z64Files, e.z64Disk, e.z64CDDisk
	} else {
		relCD, files, disk, cdDisk = uint64(e.cdOffset), uint64(e.filesThisDisk), uint32(e.disk), uint32(e.cdDisk)
	}
	if relCD > math.MaxUint64-e.archiveOffset {
		return nil, errZipInvalid
	}
	dirStart := relCD + e.archiveOffset
	if disk != cdDisk {
		return nil, errZipInvalid
	}
	r := at(b, dirStart)
	var out []zipEntry
	for range files {
		ent, err := centralEntry(r, e.archiveOffset)
		if err != nil {
			return nil, err
		}
		if ent.headerStart >= dirStart {
			return nil, errZipInvalid
		}
		out = append(out, ent)
	}
	return out, nil
}

// centralEntry parses one central directory entry at r (central_header_to_zip_file).
func centralEntry(r *le, archiveOffset uint64) (zipEntry, error) {
	if sig := r.take(4); r.err != nil || !bytes.Equal(sig, sigCentral) {
		if r.err != nil {
			return zipEntry{}, r.err
		}
		return zipEntry{}, errZipInvalid
	}
	r.u16() // version made by
	r.u16() // version needed
	var e zipEntry
	e.flags = r.u16()
	e.method = r.u16()
	r.u16() // time
	r.u16() // date
	e.crc = r.u32()
	e.csize = uint64(r.u32())
	e.usize = uint64(r.u32())
	nameLen, extraLen, commentLen := r.u16(), r.u16(), r.u16()
	r.u16() // disk
	r.u16() // internal attributes
	r.u32() // external attributes
	e.headerStart = uint64(r.u32())
	nameRaw := r.take(int(nameLen))
	extra := r.take(int(extraLen))
	commentRaw := r.take(int(commentLen))
	if r.err != nil {
		return zipEntry{}, r.err
	}
	utf8Flag := e.flags&(1<<11) != 0
	decode := func(raw []byte) string {
		if utf8Flag {
			return lossy(raw)
		}
		return cp437(string(raw))
	}
	e.name = decode(nameRaw)
	comment := decode(commentRaw)
	if err := e.parseExtra(extra, nameRaw, comment); err != nil && !errors.Is(err, errZipIO) {
		return zipEntry{}, err
	}
	if e.method == zipMethodAES && !e.aes {
		return zipEntry{}, errZipInvalid
	}
	if e.headerStart > math.MaxUint64-archiveOffset {
		return zipEntry{}, errZipInvalid
	}
	e.headerStart += archiveOffset
	return e, nil
}

// parseExtra applies the extra fields the crate reads. A field cut short (errZipIO) stops the
// parse but keeps what was read; any other error fails the archive.
func (e *zipEntry) parseExtra(extra, nameRaw []byte, comment string) error {
	// A Unicode field is checked against the name or comment as it stands, which an earlier
	// Unicode field may have replaced.
	r := &le{b: extra}
	for r.pos < len(extra) {
		kind, n := r.u16(), r.u16()
		if r.err != nil {
			return r.err
		}
		switch kind {
		case 0x0001: // ZIP64
			// Each value replaces the 32-bit one only once it was read whole.
			consumed := 0
			for _, f := range []*uint64{&e.usize, &e.csize, &e.headerStart} {
				if n >= 24 || *f == math.MaxUint32 {
					v := r.u64()
					if r.err != nil {
						return r.err
					}
					*f = v
					consumed += 8
				}
			}
			if int(n) < consumed {
				return errZipInvalid
			}
			r.take(int(n) - consumed)
		case 0x000a: // NTFS
			if n != 32 {
				return errZipInvalid
			}
			r.u32()
			tag := r.u16()
			if r.err == nil && tag != 1 {
				return errZipInvalid
			}
			size := r.u16()
			if r.err == nil && size != 24 {
				return errZipInvalid
			}
			r.take(24)
		case 0x9901: // AES
			if n != 7 {
				return errZipInvalid
			}
			version, vendor := r.u16(), r.u16()
			mode := r.u8()
			method := r.u16()
			if r.err != nil {
				return r.err
			}
			if vendor != 0x4541 || (version != 1 && version != 2) || mode < 1 || mode > 3 {
				return errZipInvalid
			}
			e.aes = true
			e.method = method
		case 0x5455: // extended timestamp
			flags := r.u8()
			if r.err != nil {
				return r.err
			}
			if n != 5 && uint32(n) != 1+4*uint32(popcount(flags)) {
				return errZipInvalid
			}
			if flags&0xF8 != 0 {
				return errZipInvalid
			}
			if flags&1 != 0 || n == 5 {
				r.u32()
			}
			if flags&2 != 0 && n > 5 {
				r.u32()
			}
			if flags&4 != 0 && n > 5 {
				r.u32()
			}
		case 0x6375, 0x7075: // Info-ZIP Unicode comment, path
			r.u8()
			crc := r.u32()
			if r.err != nil {
				return r.err
			}
			if n < 5 {
				return errZipInvalid
			}
			content := r.take(int(n) - 5)
			if r.err != nil {
				return r.err
			}
			against := nameRaw
			if kind == 0x6375 {
				against = []byte(comment)
			}
			if crc32.ChecksumIEEE(against) != crc {
				return errZipInvalid
			}
			if !utf8.Valid(content) {
				return errZipInvalid
			}
			if kind == 0x7075 {
				e.name, nameRaw = string(content), content
				e.flags |= 1 << 11
			} else {
				comment = string(content)
			}
		default:
			r.take(int(n))
		}
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

func popcount(b uint8) int {
	n := 0
	for ; b != 0; b &= b - 1 {
		n++
	}
	return n
}

// open returns the entry's decompressed data, as the crate's by_index: never encrypted, stored
// or deflated only, the local header checked.
func (e *zipEntry) open(b []byte) (io.Reader, error) {
	if e.encrypted() {
		return nil, errors.New("zip: password required")
	}
	if e.method != zipMethodStore && e.method != zipMethodDeflate {
		return nil, errors.New("zip: compression method not supported")
	}
	if e.aes {
		return nil, errors.New("zip: AES entry")
	}
	r := at(b, e.headerStart)
	sig := r.take(4)
	r.take(22)
	nameLen, extraLen := r.u16(), r.u16()
	if r.err != nil {
		return nil, r.err
	}
	if !bytes.Equal(sig, sigLocal) {
		return nil, errZipInvalid
	}
	start := e.headerStart + 30 + uint64(nameLen) + uint64(extraLen)
	end := satAdd64(start, e.csize)
	start, end = min(start, uint64(len(b))), min(end, uint64(len(b)))
	raw := bytes.NewReader(b[start:max(start, end)])
	if e.method == zipMethodStore {
		return raw, nil
	}
	// A stream cut short is an error, as in flate2 ("incomplete deflate stream").
	return flate.NewReader(raw), nil
}

// read decompresses up to room+1 bytes of the entry, checking the CRC-32 whenever the data
// ends within that.
func (e *zipEntry) read(b []byte, room uint64) ([]byte, error) {
	r, err := e.open(b)
	if err != nil {
		return nil, err
	}
	limit := min(room, math.MaxInt64-1) + 1
	buf := bytes.NewBuffer(make([]byte, 0, min(e.usize, 16<<20, limit)))
	n, err := io.Copy(buf, io.LimitReader(r, int64(limit)))
	if err != nil {
		return nil, err
	}
	if uint64(n) < limit && crc32.ChecksumIEEE(buf.Bytes()) != e.crc {
		return nil, errors.New("zip: invalid checksum")
	}
	return buf.Bytes(), nil
}
