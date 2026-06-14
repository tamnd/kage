package zim

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// maxClusterContent caps how much blob content accumulates in one cluster
// before a new one is started, balancing compression ratio against the cost of
// decompressing a whole cluster to read one small blob.
const maxClusterContent = 2 << 20 // 2 MiB

// Writer accumulates entries and serialises them as a ZIM file. Build it with
// NewWriter, add content/redirects/metadata, optionally set a main page, then
// call WriteTo. The writer holds entries in memory; a kage mirror comfortably
// fits, and packing is a one-shot batch job.
type Writer struct {
	entries    []*entry
	byKey      map[string]*entry
	mainKey    string
	noCompress bool
}

type entry struct {
	namespace byte
	url       string
	title     string
	mime      string
	data      []byte

	redirect  bool
	targetKey string // "<ns><url>" of the redirect target

	// assigned during planning
	mimeIdx     uint16
	cluster     uint32
	blob        uint32
	targetIndex uint32
	urlIndex    uint32
	position    uint64
}

func key(ns byte, url string) string { return string(ns) + url }

// NewWriter returns an empty Writer.
func NewWriter() *Writer {
	return &Writer{byKey: map[string]*entry{}}
}

// SetNoCompress stores every cluster uncompressed. Useful when the input is
// already compressed or when a reader without zstd must open the file.
func (w *Writer) SetNoCompress(v bool) { w.noCompress = v }

// AddContent adds a content entry. A later add with the same namespace and url
// replaces the earlier one. An empty title defaults to the url.
func (w *Writer) AddContent(namespace byte, url, title, mime string, data []byte) {
	if title == "" {
		title = url
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.put(&entry{namespace: namespace, url: url, title: title, mime: mime, data: data})
}

// AddMetadata adds an 'M' namespace text entry, e.g. AddMetadata("Title", "...").
func (w *Writer) AddMetadata(name, value string) {
	w.put(&entry{namespace: NamespaceMetadata, url: name, title: name, mime: "text/plain", data: []byte(value)})
}

// AddRedirect adds a redirect from (namespace,url) to (targetNamespace,targetURL).
func (w *Writer) AddRedirect(namespace byte, url, title string, targetNamespace byte, targetURL string) {
	if title == "" {
		title = url
	}
	w.put(&entry{namespace: namespace, url: url, title: title, redirect: true, targetKey: key(targetNamespace, targetURL)})
}

// SetMainPage marks an entry as the archive's entry point.
func (w *Writer) SetMainPage(namespace byte, url string) { w.mainKey = key(namespace, url) }

func (w *Writer) put(e *entry) {
	k := key(e.namespace, e.url)
	if old, ok := w.byKey[k]; ok {
		*old = *e // replace in place, keep slice order
		return
	}
	w.byKey[k] = e
	w.entries = append(w.entries, e)
}

// plan holds the prebuilt sections of the file, ready to emit in order.
type plan struct {
	hdr         header
	mimeList    []byte
	urlPtrs     []byte
	titlePtrs   []byte
	clusterPtrs []byte
	dirents     [][]byte // URL order
	clusters    [][]byte
}

// WriteTo serialises the archive to out and returns the number of bytes written.
func (w *Writer) WriteTo(out io.Writer) (int64, error) {
	p, err := w.buildPlan()
	if err != nil {
		return 0, err
	}
	sum := md5.New()
	mw := io.MultiWriter(out, sum)
	var n int64
	write := func(b []byte) error {
		m, err := mw.Write(b)
		n += int64(m)
		return err
	}
	for _, section := range append([][]byte{
		p.hdr.marshal(), p.mimeList, p.urlPtrs, p.titlePtrs, p.clusterPtrs,
	}, append(p.dirents, p.clusters...)...) {
		if err := write(section); err != nil {
			return n, err
		}
	}
	// The MD5 covers everything before it and is not itself hashed.
	m, err := out.Write(sum.Sum(nil))
	n += int64(m)
	return n, err
}

func (w *Writer) buildPlan() (plan, error) {
	var p plan
	// 1. URL order: sort by <namespace><url>, assign indices.
	ents := make([]*entry, len(w.entries))
	copy(ents, w.entries)
	sort.Slice(ents, func(i, j int) bool {
		return key(ents[i].namespace, ents[i].url) < key(ents[j].namespace, ents[j].url)
	})
	index := make(map[string]uint32, len(ents))
	for i, e := range ents {
		e.urlIndex = uint32(i)
		index[key(e.namespace, e.url)] = uint32(i)
	}

	// 2. Resolve redirect targets.
	for _, e := range ents {
		if !e.redirect {
			continue
		}
		ti, ok := index[e.targetKey]
		if !ok {
			return p, fmt.Errorf("zim: redirect %q points at missing target %q", key(e.namespace, e.url), e.targetKey)
		}
		e.targetIndex = ti
	}

	// 3. MIME list (first-seen order over content entries).
	var mimes []string
	mimeIndex := map[string]uint16{}
	for _, e := range ents {
		if e.redirect {
			continue
		}
		if _, ok := mimeIndex[e.mime]; !ok {
			mimeIndex[e.mime] = uint16(len(mimes))
			mimes = append(mimes, e.mime)
		}
		e.mimeIdx = mimeIndex[e.mime]
	}
	p.mimeList = encodeMimeList(mimes)

	// 4. Cluster packing: split text vs binary, cap each cluster, assign blobs.
	clusters := w.packClusters(ents)
	p.clusters = make([][]byte, len(clusters))
	for i, c := range clusters {
		p.clusters[i] = c.encode(w.noCompress)
	}

	// 5. Directory entry bytes (URL order).
	p.dirents = make([][]byte, len(ents))
	for i, e := range ents {
		p.dirents[i] = e.encodeDirent()
	}

	// 6. Layout: assign absolute positions.
	count := uint32(len(ents))
	pos := uint64(headerLen)
	mimeListPos := pos
	pos += uint64(len(p.mimeList))
	urlPtrPos := pos
	pos += 8 * uint64(count)
	titlePtrPos := pos
	pos += 4 * uint64(count)
	clusterPtrPos := pos
	pos += 8 * uint64(len(p.clusters))
	for i, e := range ents {
		e.position = pos
		pos += uint64(len(p.dirents[i]))
	}
	clusterPos := make([]uint64, len(p.clusters))
	for i := range p.clusters {
		clusterPos[i] = pos
		pos += uint64(len(p.clusters[i]))
	}
	checksumPos := pos

	// 7. Pointer lists.
	p.urlPtrs = make([]byte, 8*count)
	for i, e := range ents {
		binary.LittleEndian.PutUint64(p.urlPtrs[8*i:], e.position)
	}
	p.clusterPtrs = make([]byte, 8*len(clusterPos))
	for i, cp := range clusterPos {
		binary.LittleEndian.PutUint64(p.clusterPtrs[8*i:], cp)
	}
	p.titlePtrs = encodeTitlePtrs(ents)

	// 8. Header.
	p.hdr = header{
		uuid:          deriveUUID(ents),
		articleCount:  count,
		clusterCount:  uint32(len(p.clusters)),
		urlPtrPos:     urlPtrPos,
		titlePtrPos:   titlePtrPos,
		clusterPtrPos: clusterPtrPos,
		mimeListPos:   mimeListPos,
		mainPage:      noMainPage,
		layoutPage:    noMainPage,
		checksumPos:   checksumPos,
	}
	if w.mainKey != "" {
		if mi, ok := index[w.mainKey]; ok {
			p.hdr.mainPage = mi
		}
	}
	return p, nil
}

// clusterBuf accumulates blobs destined for one cluster.
type clusterBuf struct {
	comp  uint8
	blobs [][]byte
	size  int
}

func (w *Writer) packClusters(ents []*entry) []*clusterBuf {
	var clusters []*clusterBuf
	var curText, curBin *clusterBuf

	closeIf := func(c **clusterBuf) {
		if *c != nil && (*c).size >= maxClusterContent {
			*c = nil
		}
	}
	add := func(cur **clusterBuf, comp uint8, e *entry) {
		if *cur == nil {
			*cur = &clusterBuf{comp: comp}
			clusters = append(clusters, *cur)
		}
		c := *cur
		e.cluster = uint32(indexOf(clusters, c))
		e.blob = uint32(len(c.blobs))
		c.blobs = append(c.blobs, e.data)
		c.size += len(e.data)
	}

	for _, e := range ents {
		if e.redirect {
			continue
		}
		if isTextMime(e.mime) {
			add(&curText, compZstd, e)
			closeIf(&curText)
		} else {
			add(&curBin, compNone, e)
			closeIf(&curBin)
		}
	}
	return clusters
}

func indexOf(cs []*clusterBuf, c *clusterBuf) int {
	for i := range cs {
		if cs[i] == c {
			return i
		}
	}
	return -1
}

// encode renders a cluster: an info byte followed by the (optionally zstd)
// data section, which is an offset table of (N+1) uint32 values then the N
// concatenated blobs. Offsets are relative to the start of the data section.
func (c *clusterBuf) encode(noCompress bool) []byte {
	n := len(c.blobs)
	tableLen := 4 * (n + 1)
	total := tableLen
	for _, b := range c.blobs {
		total += len(b)
	}
	data := make([]byte, tableLen, total)
	off := uint32(tableLen)
	binary.LittleEndian.PutUint32(data[0:], off)
	for i, b := range c.blobs {
		off += uint32(len(b))
		binary.LittleEndian.PutUint32(data[4*(i+1):], off)
	}
	for _, b := range c.blobs {
		data = append(data, b...)
	}

	comp := c.comp
	if noCompress {
		comp = compNone
	}
	payload := data
	if comp == compZstd {
		payload = zstdEncode(data)
	} else {
		comp = compNone
	}
	out := make([]byte, 0, len(payload)+1)
	out = append(out, comp) // non-extended: bit 4 clear, uint32 offsets
	return append(out, payload...)
}

func (e *entry) encodeDirent() []byte {
	le := binary.LittleEndian
	var head []byte
	if e.redirect {
		head = make([]byte, 12)
		le.PutUint16(head[0:], redirectEntry)
		head[3] = e.namespace
		le.PutUint32(head[8:], e.targetIndex)
	} else {
		head = make([]byte, 16)
		le.PutUint16(head[0:], e.mimeIdx)
		head[3] = e.namespace
		le.PutUint32(head[8:], e.cluster)
		le.PutUint32(head[12:], e.blob)
	}
	out := append(head, e.url...)
	out = append(out, 0)
	out = append(out, e.title...)
	return append(out, 0)
}

func encodeMimeList(mimes []string) []byte {
	var b []byte
	for _, m := range mimes {
		b = append(b, m...)
		b = append(b, 0)
	}
	return append(b, 0) // terminating empty string
}

func encodeTitlePtrs(ents []*entry) []byte {
	order := make([]*entry, len(ents))
	copy(order, ents)
	sort.Slice(order, func(i, j int) bool {
		ti := string(order[i].namespace) + order[i].title
		tj := string(order[j].namespace) + order[j].title
		if ti != tj {
			return ti < tj
		}
		return order[i].urlIndex < order[j].urlIndex
	})
	b := make([]byte, 4*len(order))
	for i, e := range order {
		binary.LittleEndian.PutUint32(b[4*i:], e.urlIndex)
	}
	return b
}

// deriveUUID makes the file deterministic: identical input yields an identical
// archive. It hashes every entry's key and content, so repacking the same
// mirror is idempotent and diffable.
func deriveUUID(ents []*entry) [16]byte {
	h := md5.New()
	var n [8]byte
	for _, e := range ents {
		h.Write([]byte(key(e.namespace, e.url)))
		binary.LittleEndian.PutUint64(n[:], uint64(len(e.data)))
		h.Write(n[:])
		h.Write(e.data)
	}
	var u [16]byte
	copy(u[:], h.Sum(nil))
	return u
}
