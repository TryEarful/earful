package attach

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"path"
	"strings"
	"unicode"
)

// unpack adds each member of a zip archive as a file of its own, under
// the same allowlist and checks as an upload. Members are read one at a
// time into memory, and only after their declared size and compression
// ratio pass; the read itself is capped too, since a header can lie.
func (p *preparer) unpack(name string, data []byte) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return &Error{Reason: ReasonUnreadable, Name: name}
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() || ignoredMember(f.Name) {
			continue
		}
		member := name + "/" + memberName(f.Name)
		if len(p.out) >= p.lim.MaxFiles {
			return &Error{Reason: ReasonCount, Name: member}
		}
		if f.UncompressedSize64 > uint64(p.lim.MaxFileBytes) {
			return &Error{Reason: ReasonTooLarge, Name: member}
		}
		if expands(f.UncompressedSize64, f.CompressedSize64, p.lim.MaxRatio) {
			return &Error{Reason: ReasonBomb, Name: member}
		}
		content, err := readMember(f, p.lim.MaxFileBytes)
		if err != nil {
			return withName(err, member)
		}
		if expands(uint64(len(content)), f.CompressedSize64, p.lim.MaxRatio) {
			return &Error{Reason: ReasonBomb, Name: member}
		}
		if err := p.add(member, content, false); err != nil {
			return err
		}
	}
	return nil
}

// ignoredMember skips what archiving tools add on their own: macOS's
// resource forks and folder metadata. They are not anything the creator
// chose to send, and refusing the archive over them would be baffling.
func ignoredMember(name string) bool {
	base := path.Base(name)
	return strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(base, "._") ||
		base == ".DS_Store" || base == "Thumbs.db" || base == "desktop.ini"
}

// memberName is the member's path inside the archive, as a label only:
// it is never used to open anything.
func memberName(name string) string {
	cleaned := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, `\`, "/")), "/")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, cleaned)
}

// expands reports a compression ratio past max: a decompression bomb.
// An empty member is not one.
func expands(uncompressed, compressed uint64, max int64) bool {
	if uncompressed == 0 {
		return false
	}
	if compressed == 0 {
		return true
	}
	return uncompressed/compressed > uint64(max)
}

// readMember reads one member whole, refusing it when it holds more
// than limit bytes whatever its header says.
func readMember(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, &Error{Reason: ReasonUnreadable}
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, &Error{Reason: ReasonUnreadable}
	}
	if int64(len(content)) > limit {
		return nil, &Error{Reason: ReasonBomb}
	}
	return content, nil
}

// containerKind says which Office or OpenDocument format a zip
// container holds, by the parts the format requires: OpenDocument names
// itself in a "mimetype" member, Office Open XML by its main part. A zip
// that is neither is a plain archive.
func containerKind(data []byte) (kind, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, &Error{Reason: ReasonUnreadable}
	}
	if f := findPart(r, "mimetype"); f != nil {
		mimetype, err := readMember(f, 256)
		if err != nil {
			return 0, err
		}
		switch strings.TrimSpace(string(mimetype)) {
		case "application/vnd.oasis.opendocument.text":
			return kindODT, nil
		case "application/vnd.oasis.opendocument.presentation":
			return kindODP, nil
		}
		return kindZip, nil
	}
	if findPart(r, "[Content_Types].xml") == nil {
		return kindZip, nil
	}
	switch {
	case findPart(r, "word/document.xml") != nil:
		return kindDOCX, nil
	case findPart(r, "ppt/presentation.xml") != nil:
		return kindPPTX, nil
	case findPart(r, "xl/workbook.xml") != nil:
		return kindXLSX, nil
	}
	return kindZip, nil
}

// findPart returns the named member. Office Open XML part names are
// case-insensitive.
func findPart(r *zip.Reader, name string) *zip.File {
	for _, f := range r.File {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

// errBudget is a document whose XML runs past MaxDocumentXMLBytes.
var errBudget = errors.New("attach: document expands past its budget")

// budget is what one document may still decompress, shared across its
// parts.
type budget struct{ left int64 }

// open returns a reader over one part that draws on the budget.
func (b *budget) open(f *zip.File) (io.ReadCloser, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, &Error{Reason: ReasonUnreadable}
	}
	return &budgetReader{rc: rc, b: b}, nil
}

type budgetReader struct {
	rc io.ReadCloser
	b  *budget
}

func (r *budgetReader) Read(p []byte) (int, error) {
	if r.b.left <= 0 {
		// Spent exactly at the end of the part is not over budget.
		var probe [1]byte
		if n, err := r.rc.Read(probe[:]); n == 0 && errors.Is(err, io.EOF) {
			return 0, io.EOF
		}
		return 0, errBudget
	}
	if int64(len(p)) > r.b.left {
		p = p[:r.b.left]
	}
	n, err := r.rc.Read(p)
	r.b.left -= int64(n)
	return n, err
}

func (r *budgetReader) Close() error { return r.rc.Close() }
