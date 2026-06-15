package zim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// ErrNotFound is returned by Get when no entry matches the namespace and url.
// Callers (such as the HTTP handler) test for it with errors.Is to map a miss
// to a 404.
var ErrNotFound = errors.New("zim: not found")

// Reader provides random access to a ZIM file's entries. Open one with Open or
// NewReader, then look entries up by namespace and url, or fetch the main page.
// Decompressed clusters are cached so repeated reads from one cluster are cheap.
type Reader struct {
	ra     io.ReaderAt
	closer io.Closer
	size   int64
	hdr    header
	mimes  []string

	mu            sync.Mutex
	cache         map[uint32][]byte // cluster index -> decompressed data section
	cacheExtended map[uint32]bool   // cluster index -> uint64-offset cluster
}

// Blob is the result of a lookup: the resolved entry's bytes and metadata.
type Blob struct {
	Namespace byte
	URL       string
	Title     string
	MimeType  string
	Data      []byte
}

// Open opens a ZIM file on disk. Close the returned reader when done.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	r, err := NewReader(f, fi.Size())
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	r.closer = f
	return r, nil
}

// NewReader reads the header and MIME list from ra, which must hold size bytes.
func NewReader(ra io.ReaderAt, size int64) (*Reader, error) {
	r := &Reader{ra: ra, size: size, cache: map[uint32][]byte{}}
	hb, err := r.at(0, headerLen)
	if err != nil {
		return nil, fmt.Errorf("zim: read header: %w", err)
	}
	r.hdr, err = parseHeader(hb)
	if err != nil {
		return nil, err
	}
	if r.hdr.mimeListPos > r.hdr.urlPtrPos || r.hdr.urlPtrPos > uint64(size) {
		return nil, fmt.Errorf("zim: inconsistent header offsets")
	}
	mb, err := r.at(r.hdr.mimeListPos, int(r.hdr.urlPtrPos-r.hdr.mimeListPos))
	if err != nil {
		return nil, fmt.Errorf("zim: read mime list: %w", err)
	}
	for _, part := range bytes.Split(mb, []byte{0}) {
		if len(part) == 0 {
			break
		}
		r.mimes = append(r.mimes, string(part))
	}
	return r, nil
}

// Close releases the underlying file, if Open created one.
func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// Count returns the number of directory entries.
func (r *Reader) Count() uint32 { return r.hdr.articleCount }

// MimeTypes returns the archive's MIME-type list.
func (r *Reader) MimeTypes() []string { return r.mimes }

// MainPage returns the archive's entry point, or an error if none is set.
func (r *Reader) MainPage() (Blob, error) {
	if r.hdr.mainPage == noMainPage {
		return Blob{}, fmt.Errorf("zim: no main page")
	}
	return r.blobAtIndex(r.hdr.mainPage, 0)
}

// Entry is one directory entry as stored, returned by EntryAt. A redirect keeps
// Data nil and names its target in RedirectNamespace/RedirectURL; any other
// entry carries its bytes in Data and its type in MimeType. Unlike Get, EntryAt
// does not follow redirects, so a caller can round-trip every entry, the
// redirects included.
type Entry struct {
	Namespace         byte
	URL               string
	Title             string
	MimeType          string
	Redirect          bool
	RedirectNamespace byte
	RedirectURL       string
	Data              []byte
}

// EntryAt returns the directory entry at idx, where 0 <= idx < Count, in the
// archive's URL order. It is the iteration counterpart to Get: it exposes every
// entry exactly as stored, including metadata and redirects, which is what an
// exporter needs to reproduce the archive.
func (r *Reader) EntryAt(idx uint32) (Entry, error) {
	d, err := r.direntAtIndex(idx)
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Namespace: d.namespace, URL: d.url, Title: d.title}
	if d.redirect {
		e.Redirect = true
		td, err := r.direntAtIndex(d.targetIndex)
		if err != nil {
			return Entry{}, fmt.Errorf("zim: redirect target of %c/%s: %w", d.namespace, d.url, err)
		}
		e.RedirectNamespace = td.namespace
		e.RedirectURL = td.url
		return e, nil
	}
	data, err := r.blobData(d.cluster, d.blob)
	if err != nil {
		return Entry{}, err
	}
	if int(d.mimeIdx) < len(r.mimes) {
		e.MimeType = r.mimes[d.mimeIdx]
	}
	e.Data = data
	return e, nil
}

// MainPageRef returns the namespace and url of the archive's entry point and
// whether one is set, so an exporter can record which entry is the main page
// without following the W/mainPage redirect.
func (r *Reader) MainPageRef() (byte, string, bool) {
	if r.hdr.mainPage == noMainPage {
		return 0, "", false
	}
	d, err := r.direntAtIndex(r.hdr.mainPage)
	if err != nil {
		return 0, "", false
	}
	return d.namespace, d.url, true
}

// Get resolves the entry at (namespace, url), following one or more redirects.
func (r *Reader) Get(namespace byte, url string) (Blob, error) {
	target := key(namespace, url)
	lo, hi := uint32(0), r.hdr.articleCount
	for lo < hi {
		mid := lo + (hi-lo)/2
		d, err := r.direntAtIndex(mid)
		if err != nil {
			return Blob{}, err
		}
		switch k := key(d.namespace, d.url); {
		case k < target:
			lo = mid + 1
		case k > target:
			hi = mid
		default:
			return r.blobAtIndex(mid, 0)
		}
	}
	return Blob{}, fmt.Errorf("%w: %c/%s", ErrNotFound, namespace, url)
}

const maxRedirectHops = 16

func (r *Reader) blobAtIndex(idx uint32, hop int) (Blob, error) {
	if hop > maxRedirectHops {
		return Blob{}, fmt.Errorf("zim: redirect loop")
	}
	d, err := r.direntAtIndex(idx)
	if err != nil {
		return Blob{}, err
	}
	if d.redirect {
		return r.blobAtIndex(d.targetIndex, hop+1)
	}
	data, err := r.blobData(d.cluster, d.blob)
	if err != nil {
		return Blob{}, err
	}
	mime := ""
	if int(d.mimeIdx) < len(r.mimes) {
		mime = r.mimes[d.mimeIdx]
	}
	return Blob{Namespace: d.namespace, URL: d.url, Title: d.title, MimeType: mime, Data: data}, nil
}

type dirent struct {
	mimeIdx     uint16
	namespace   byte
	url, title  string
	cluster     uint32
	blob        uint32
	redirect    bool
	targetIndex uint32
}

func (r *Reader) direntAtIndex(idx uint32) (dirent, error) {
	pb, err := r.at(r.hdr.urlPtrPos+8*uint64(idx), 8)
	if err != nil {
		return dirent{}, err
	}
	return r.direntAt(binary.LittleEndian.Uint64(pb))
}

func (r *Reader) direntAt(off uint64) (dirent, error) {
	// Read a window large enough for the fixed head plus url and title; grow if
	// either string is not terminated within it.
	window := 512
	for {
		b, err := r.at(off, window)
		if err != nil && len(b) == 0 {
			return dirent{}, err
		}
		var d dirent
		le := binary.LittleEndian
		d.mimeIdx = le.Uint16(b[0:])
		d.namespace = b[3]
		var p int
		if d.mimeIdx == redirectEntry {
			d.redirect = true
			d.targetIndex = le.Uint32(b[8:])
			p = 12
		} else {
			d.cluster = le.Uint32(b[8:])
			d.blob = le.Uint32(b[12:])
			p = 16
		}
		url, n1, ok := readCString(b, p)
		if !ok {
			if window >= 1<<20 || off+uint64(window) >= uint64(r.size) {
				return dirent{}, fmt.Errorf("zim: unterminated url at %d", off)
			}
			window *= 4
			continue
		}
		title, _, ok := readCString(b, n1)
		if !ok {
			if window >= 1<<20 || off+uint64(window) >= uint64(r.size) {
				return dirent{}, fmt.Errorf("zim: unterminated title at %d", off)
			}
			window *= 4
			continue
		}
		d.url, d.title = url, title
		return d, nil
	}
}

// blobData returns one blob's bytes, decompressing and caching its cluster.
func (r *Reader) blobData(cluster, blob uint32) ([]byte, error) {
	data, extended, err := r.clusterData(cluster)
	if err != nil {
		return nil, err
	}
	w := uint32(4)
	if extended {
		w = 8
	}
	need := int((blob + 2) * w)
	if need > len(data) {
		return nil, fmt.Errorf("zim: blob %d out of range in cluster %d", blob, cluster)
	}
	o0 := readUint(data[blob*w:], w)
	o1 := readUint(data[(blob+1)*w:], w)
	if o0 > o1 || int(o1) > len(data) {
		return nil, fmt.Errorf("zim: bad blob offsets in cluster %d", cluster)
	}
	out := make([]byte, o1-o0)
	copy(out, data[o0:o1])
	return out, nil
}

func (r *Reader) clusterData(cluster uint32) (data []byte, extended bool, err error) {
	r.mu.Lock()
	if c, ok := r.cache[cluster]; ok {
		r.mu.Unlock()
		// extended-ness is recoverable from the info byte, but the cache stores
		// already-decoded data whose offsets we re-read with the recorded width.
		return c, r.cacheExtended[cluster], nil
	}
	r.mu.Unlock()

	start, err := r.clusterOffset(cluster)
	if err != nil {
		return nil, false, err
	}
	end := r.hdr.checksumPos
	if cluster+1 < r.hdr.clusterCount {
		if end, err = r.clusterOffset(cluster + 1); err != nil {
			return nil, false, err
		}
	}
	if start >= end || end > uint64(r.size) {
		return nil, false, fmt.Errorf("zim: bad cluster bounds for %d", cluster)
	}
	raw, err := r.at(start, int(end-start))
	if err != nil {
		return nil, false, err
	}
	info := raw[0]
	comp := info & 0x0f
	extended = info&extendedFlag != 0
	body := raw[1:]
	switch comp {
	case compNone:
		data = body
	case compZstd:
		if data, err = zstdDecode(body); err != nil {
			return nil, false, fmt.Errorf("zim: zstd cluster %d: %w", cluster, err)
		}
	case compXZ:
		return nil, false, fmt.Errorf("zim: xz clusters are not supported for reading")
	default:
		return nil, false, fmt.Errorf("zim: unknown compression %d in cluster %d", comp, cluster)
	}
	r.mu.Lock()
	r.cache[cluster] = data
	if r.cacheExtended == nil {
		r.cacheExtended = map[uint32]bool{}
	}
	r.cacheExtended[cluster] = extended
	r.mu.Unlock()
	return data, extended, nil
}

func (r *Reader) clusterOffset(cluster uint32) (uint64, error) {
	b, err := r.at(r.hdr.clusterPtrPos+8*uint64(cluster), 8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

// at reads n bytes at off, clamped to the file size.
func (r *Reader) at(off uint64, n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("zim: negative read length")
	}
	if off > uint64(r.size) {
		return nil, io.EOF
	}
	if off+uint64(n) > uint64(r.size) {
		n = int(uint64(r.size) - off)
	}
	b := make([]byte, n)
	if n == 0 {
		return b, nil
	}
	_, err := r.ra.ReadAt(b, int64(off))
	return b, err
}

func readCString(b []byte, start int) (string, int, bool) {
	if start > len(b) {
		return "", start, false
	}
	i := bytes.IndexByte(b[start:], 0)
	if i < 0 {
		return "", start, false
	}
	return string(b[start : start+i]), start + i + 1, true
}

func readUint(b []byte, width uint32) uint32 {
	if width == 8 {
		return uint32(binary.LittleEndian.Uint64(b))
	}
	return binary.LittleEndian.Uint32(b)
}
