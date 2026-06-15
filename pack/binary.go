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

	payload := assemble(baseBytes, zimBytes)
	if err := os.WriteFile(opts.Out, payload, 0o755); err != nil {
		return opts.Out, 0, err
	}
	// WriteFile honours the mode only when it creates the file; chmod makes an
	// overwrite executable too.
	if err := os.Chmod(opts.Out, 0o755); err != nil {
		return opts.Out, 0, err
	}
	return opts.Out, int64(len(payload)), nil
}

// assemble builds the self-contained viewer image: the base executable, then the
// ZIM archive, then the KAGEPCK1 trailer that records the archive length. ELF,
// PE, and Mach-O loaders all ignore trailing bytes, so the result still runs on
// its target OS while Embedded finds the archive at the tail.
func assemble(baseBytes, zimBytes []byte) []byte {
	var tr bytes.Buffer
	tr.WriteString(trailerMagic)
	_ = binary.Write(&tr, binary.LittleEndian, uint64(len(zimBytes)))
	tr.WriteString(trailerMagic)

	out := make([]byte, 0, len(baseBytes)+len(zimBytes)+tr.Len())
	out = append(out, baseBytes...)
	out = append(out, zimBytes...)
	out = append(out, tr.Bytes()...)
	return out
}
