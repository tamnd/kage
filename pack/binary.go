package pack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	// trailerMagic brackets the appended-archive trailer at both ends, so a stray
	// copy of it inside the base binary cannot be mistaken for a real trailer.
	trailerMagic = "KAGEPCK1"
	// trailerLen is magic + uint64 archive length + magic again.
	trailerLen = len(trailerMagic) + 8 + len(trailerMagic)
)

// BinaryOptions controls how a self-contained viewer is assembled.
type BinaryOptions struct {
	Out  string // output path
	Base string // base kage binary; default os.Executable()
}

// BuildBinary writes baseExe ++ zimBytes ++ trailer to opts.Out and marks it
// executable. The base must be a kage binary, since the viewer behaviour lives
// in kage's own startup hook (see Embedded); appending a ZIM to an arbitrary
// executable would only produce a broken file. It returns the output path and
// the total byte size.
func BuildBinary(zimBytes []byte, opts BinaryOptions) (string, int64, error) {
	base := opts.Base
	if base == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", 0, fmt.Errorf("pack: locate base binary: %w", err)
		}
		base = exe
	}
	if opts.Out == "" {
		return "", 0, fmt.Errorf("pack: BuildBinary requires an output path")
	}

	baseBytes, err := os.ReadFile(base)
	if err != nil {
		return "", 0, fmt.Errorf("pack: read base binary %q: %w", base, err)
	}

	var tr bytes.Buffer
	tr.WriteString(trailerMagic)
	_ = binary.Write(&tr, binary.LittleEndian, uint64(len(zimBytes)))
	tr.WriteString(trailerMagic)

	f, err := os.Create(opts.Out)
	if err != nil {
		return "", 0, err
	}
	for _, chunk := range [][]byte{baseBytes, zimBytes, tr.Bytes()} {
		if _, err := f.Write(chunk); err != nil {
			_ = f.Close()
			return opts.Out, 0, err
		}
	}
	if err := f.Close(); err != nil {
		return opts.Out, 0, err
	}
	if err := os.Chmod(opts.Out, 0o755); err != nil {
		return opts.Out, 0, err
	}
	return opts.Out, int64(len(baseBytes) + len(zimBytes) + trailerLen), nil
}
