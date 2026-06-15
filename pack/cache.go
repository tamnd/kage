package pack

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
	"sort"

	"github.com/tamnd/kage/zim"
)

// cacheMagic tags the sidecar so a stale or foreign file is recognised and
// ignored rather than misread. The trailing digit is the format version.
var cacheMagic = [8]byte{'k', 'a', 'g', 'e', 'z', 'c', 'h', '1'}

// clusterCache is a content-addressed store of compressed ZIM clusters, kept in
// a sidecar next to the archive. Compressing clusters with zstd is the dominant
// cost of packing a large mirror, so a re-pack reuses the compression of every
// cluster whose uncompressed bytes are unchanged and only compresses the rest.
//
// A cache hit returns exactly what a fresh compression would have produced, so
// the archive stays deterministic and valid; the cache only saves CPU. Clusters
// are keyed by the SHA-256 of their uncompressed data section, which the zim
// writer assembles before compression.
type clusterCache struct {
	prev map[[32]byte][]byte // clusters loaded from the previous pack
	used map[[32]byte][]byte // clusters touched this pack, written back on save

	reused     int // clusters served from the cache this pack
	compressed int // clusters compressed fresh this pack
}

func newClusterCache() *clusterCache {
	return &clusterCache{prev: map[[32]byte][]byte{}, used: map[[32]byte][]byte{}}
}

// loadClusterCache reads the sidecar at path. A missing, unreadable, or
// truncated cache is not an error: packing starts cold and rebuilds it.
func loadClusterCache(path string) *clusterCache {
	c := newClusterCache()
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil || magic != cacheMagic {
		return c
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return c
	}
	for i := uint32(0); i < count; i++ {
		var h [32]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return newClusterCache() // truncated: drop the partial load
		}
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			return newClusterCache()
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			return newClusterCache()
		}
		c.prev[h] = b
	}
	return c
}

// Compress returns the stored bytes for an uncompressed cluster: the cached
// compression when the cluster's bytes are unchanged, a fresh zstd compression
// on a miss. It is the function handed to zim.Writer.SetCompress.
func (c *clusterCache) Compress(data []byte) []byte {
	h := sha256.Sum256(data)
	if b, ok := c.used[h]; ok {
		return b
	}
	if b, ok := c.prev[h]; ok {
		c.used[h] = b
		c.reused++
		return b
	}
	b := zim.Compress(data)
	c.used[h] = b
	c.compressed++
	return b
}

// save writes the clusters touched this pack to path, replacing the previous
// sidecar. Only touched clusters are written, so clusters that left the mirror
// drop out and the cache cannot grow without bound. Entries are sorted by hash
// so the sidecar itself is reproducible. The write goes through a temp file and
// a rename so a crash mid-write cannot corrupt an existing cache.
func (c *clusterCache) save(path string) error {
	hashes := make([][32]byte, 0, len(c.used))
	for h := range c.used {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool {
		return bytes.Compare(hashes[i][:], hashes[j][:]) < 0
	})

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	_, _ = w.Write(cacheMagic[:])
	_ = binary.Write(w, binary.LittleEndian, uint32(len(hashes)))
	for _, h := range hashes {
		b := c.used[h]
		_, _ = w.Write(h[:])
		_ = binary.Write(w, binary.LittleEndian, uint32(len(b)))
		_, _ = w.Write(b)
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
