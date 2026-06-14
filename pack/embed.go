package pack

import (
	"encoding/binary"
	"io"
	"os"
)

// Embedded inspects the running executable for an appended ZIM archive. If the
// KAGEPCK1 trailer is present, it returns a ReaderAt bounded to the archive, its
// size, and ok=true; the file handle stays open for the life of the process so
// the viewer can serve from it. A normal kage build has no trailer, so the cost
// to every ordinary invocation is one Open plus a 24-byte ReadAt.
func Embedded() (ra io.ReaderAt, size int64, ok bool) {
	exe, err := os.Executable()
	if err != nil {
		return nil, 0, false
	}
	f, err := os.Open(exe)
	if err != nil {
		return nil, 0, false
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, false
	}
	total := info.Size()
	if total < int64(trailerLen) {
		_ = f.Close()
		return nil, 0, false
	}

	tr := make([]byte, trailerLen)
	if _, err := f.ReadAt(tr, total-int64(trailerLen)); err != nil {
		_ = f.Close()
		return nil, 0, false
	}
	if string(tr[:8]) != trailerMagic || string(tr[trailerLen-8:]) != trailerMagic {
		_ = f.Close()
		return nil, 0, false
	}
	zlen := int64(binary.LittleEndian.Uint64(tr[8:16]))
	start := total - int64(trailerLen) - zlen
	if zlen <= 0 || start < 0 {
		_ = f.Close()
		return nil, 0, false
	}
	return io.NewSectionReader(f, start, zlen), zlen, true
}
