// Package zim reads and writes the ZIM offline-archive format, the open
// single-file container that Kiwix uses to ship offline content. kage uses it
// to pack a cloned mirror into one indexable, compressed file that a reader can
// random-access without unpacking.
//
// The package is pure: no network, no clock, no global state beyond a lazily
// built zstd codec. A ZIM file is laid out as a fixed header, a MIME-type list,
// three pointer lists (URL, title, cluster), a run of directory entries, a run
// of clusters that hold the content, and a trailing MD5. Every cross-reference
// is an absolute file position recorded in the header, so the writer assigns
// positions in one pass and emits bytes in a second. All integers are
// little-endian.
//
// We write the new namespace scheme (minor version 1): all content lives under
// the single 'C' namespace, metadata under 'M', and a 'W/mainPage' redirect
// points at the entry point. Reading handles redirects and both offset widths.
package zim

import (
	"encoding/binary"
	"fmt"
)

// Magic is the ZIM header magic number, the first four bytes of every file.
const Magic uint32 = 0x44D495A // 72173914

const (
	majorVersion uint16 = 5
	minorVersion uint16 = 1 // single 'C' content namespace
	headerLen           = 80
)

// Namespaces in the new (minor version 1) scheme.
const (
	NamespaceContent   byte = 'C' // pages and assets
	NamespaceMetadata  byte = 'M' // M/Title, M/Date, ...
	NamespaceWellKnown byte = 'W' // W/mainPage redirect
)

// Compression codes carried in the low nibble of a cluster's info byte.
const (
	compNone uint8 = 1 // stored, no compression
	compXZ   uint8 = 4 // xz / LZMA2 (read-only support)
	compZstd uint8 = 5 // zstd (what we write for text)

	extendedFlag uint8 = 0x10 // bit 4: cluster offsets are uint64, not uint32
)

// Sentinels stored in a directory entry's mimetype field to mark non-content
// entries. A redirect reuses the cluster slot to hold its target's URL index.
const (
	redirectEntry   uint16 = 0xffff
	linkTargetEntry uint16 = 0xfffe
	deletedEntry    uint16 = 0xfffd
)

// noMainPage is the mainPage/layoutPage value meaning "none".
const noMainPage uint32 = 0xffffffff

// header is the 80-byte ZIM header.
type header struct {
	uuid          [16]byte
	articleCount  uint32
	clusterCount  uint32
	urlPtrPos     uint64
	titlePtrPos   uint64
	clusterPtrPos uint64
	mimeListPos   uint64
	mainPage      uint32
	layoutPage    uint32
	checksumPos   uint64
}

// marshal encodes the header to its 80 wire bytes.
func (h header) marshal() []byte {
	b := make([]byte, headerLen)
	le := binary.LittleEndian
	le.PutUint32(b[0:], Magic)
	le.PutUint16(b[4:], majorVersion)
	le.PutUint16(b[6:], minorVersion)
	copy(b[8:24], h.uuid[:])
	le.PutUint32(b[24:], h.articleCount)
	le.PutUint32(b[28:], h.clusterCount)
	le.PutUint64(b[32:], h.urlPtrPos)
	le.PutUint64(b[40:], h.titlePtrPos)
	le.PutUint64(b[48:], h.clusterPtrPos)
	le.PutUint64(b[56:], h.mimeListPos)
	le.PutUint32(b[64:], h.mainPage)
	le.PutUint32(b[68:], h.layoutPage)
	le.PutUint64(b[72:], h.checksumPos)
	return b
}

// parseHeader decodes and validates an 80-byte header.
func parseHeader(b []byte) (header, error) {
	var h header
	if len(b) < headerLen {
		return h, fmt.Errorf("zim: short header: %d bytes", len(b))
	}
	le := binary.LittleEndian
	if le.Uint32(b[0:]) != Magic {
		return h, fmt.Errorf("zim: bad magic, not a ZIM file")
	}
	copy(h.uuid[:], b[8:24])
	h.articleCount = le.Uint32(b[24:])
	h.clusterCount = le.Uint32(b[28:])
	h.urlPtrPos = le.Uint64(b[32:])
	h.titlePtrPos = le.Uint64(b[40:])
	h.clusterPtrPos = le.Uint64(b[48:])
	h.mimeListPos = le.Uint64(b[56:])
	h.mainPage = le.Uint32(b[64:])
	h.layoutPage = le.Uint32(b[68:])
	h.checksumPos = le.Uint64(b[72:])
	return h, nil
}
