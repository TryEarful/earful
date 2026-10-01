package attach_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/attach"
)

// Fixtures are built here, in memory, rather than checked in: each is
// the smallest file of its format that a real reader accepts, which
// keeps what is being tested visible in the test.

type member struct {
	name  string
	data  []byte
	store bool // stored rather than deflated
}

func zipOf(t *testing.T, members ...member) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		method := zip.Deflate
		if m.store {
			method = zip.Store
		}
		f, err := w.CreateHeader(&zip.FileHeader{Name: m.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`

func docx(t *testing.T) []byte {
	return zipOf(t,
		member{name: "[Content_Types].xml", data: []byte(contentTypes)},
		member{name: "word/document.xml", data: []byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="720"/></w:tabs></w:pPr><w:r><w:t>Onboarding </w:t></w:r><w:r><w:t>questions</w:t></w:r></w:p>
<w:p><w:r><w:t>How was</w:t><w:tab/><w:t>week one?</w:t></w:r></w:p>
</w:body></w:document>`)},
	)
}

func pptx(t *testing.T) []byte {
	slide := func(text string) []byte {
		return []byte(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`)
	}
	return zipOf(t,
		member{name: "[Content_Types].xml", data: []byte(contentTypes)},
		member{name: "ppt/presentation.xml", data: []byte(`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`)},
		member{name: "ppt/slides/slide10.xml", data: slide("Tenth slide")},
		member{name: "ppt/slides/slide2.xml", data: slide("Second slide")},
	)
}

func xlsx(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName("Sheet1", "Questions")
	_ = f.SetCellValue("Questions", "A1", "Question")
	_ = f.SetCellValue("Questions", "B1", "Answer")
	_ = f.SetCellValue("Questions", "A2", "How did you hear of us?")
	_ = f.SetCellValue("Questions", "B2", "A friend")
	_ = f.SetCellValue("Questions", "C2", 42)
	if _, err := f.NewSheet("Notes"); err != nil {
		t.Fatal(err)
	}
	_ = f.SetCellValue("Notes", "A1", "Keep it short")
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func odt(t *testing.T, mimetype string) []byte {
	return zipOf(t,
		member{name: "mimetype", data: []byte(mimetype), store: true},
		member{name: "content.xml", data: []byte(`<?xml version="1.0" encoding="UTF-8"?>
<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">
<office:automatic-styles><text:p>style noise</text:p></office:automatic-styles>
<office:body><office:text><text:h>Team survey</text:h><text:p>Ask about<text:s/>meetings</text:p></office:text></office:body>
</office:document-content>`)},
	)
}

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 32)...)

func prepare(t *testing.T, files ...attach.File) ([]ai.Attachment, error) {
	t.Helper()
	return attach.Prepare(context.Background(), files, attach.Options{})
}

func refusal(t *testing.T, err error, want attach.Reason) *attach.Error {
	t.Helper()
	var e *attach.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a refusal for %s", err, want)
	}
	if e.Reason != want {
		t.Fatalf("reason = %s (%s), want %s", e.Reason, e.Name, want)
	}
	return e
}

func TestPrepare_ReadsEachFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		data     []byte
		mime     string
		contains []string
		absent   []string
	}{
		{"notes.md", []byte("# Goals\n\n- learn"), ai.MIMEText, []string{"# Goals\n\n- learn"}, nil},
		{"data.csv", []byte("q,a\nWhy,Because\n"), ai.MIMEText, []string{"q,a\nWhy,Because"}, nil},
		{"brief.pdf", []byte("%PDF-1.4\n1 0 obj\n"), ai.MIMEPDF, nil, nil},
		{"photo.png", pngBytes, ai.MIMEPNG, nil, nil},
		{"photo.JPG", append([]byte("\xff\xd8\xff\xe0"), make([]byte, 16)...), ai.MIMEJPEG, nil, nil},
		{"brief.docx", docx(t), ai.MIMEText, []string{"Onboarding questions", "How was\tweek one?"}, nil},
		{"deck.pptx", pptx(t), ai.MIMEText, []string{"Second slide\n\nTenth slide"}, nil},
		{"answers.xlsx", xlsx(t), ai.MIMEText, []string{"Sheet: Questions", "Question\tAnswer", "How did you hear of us?\tA friend\t42", "Sheet: Notes", "Keep it short"}, nil},
		{"plan.odt", odt(t, "application/vnd.oasis.opendocument.text"), ai.MIMEText, []string{"Team survey", "Ask about meetings"}, []string{"style noise"}},
		{"talk.odp", odt(t, "application/vnd.oasis.opendocument.presentation"), ai.MIMEText, []string{"Ask about meetings"}, nil},
		{"page.html", []byte(`<!DOCTYPE html><html><head><title>Page</title><style>p{}</style><script>alert(1)</script></head><body><p>Hello &amp; welcome</p><p>Second</p></body></html>`), ai.MIMEText, []string{"Hello & welcome\n", "Second"}, []string{"alert", "p{}", "<p>"}},
		{"chart.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><title>Sales</title><script>alert(1)</script><text x="1">Q1 &amp; Q2</text></svg>`), ai.MIMEText, []string{"Sales", "Q1 & Q2"}, []string{"alert", "<svg"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := prepare(t, attach.File{Name: c.name, Data: c.data})
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if len(out) != 1 || out[0].Name != c.name || out[0].MIME != c.mime {
				t.Fatalf("got %+v", out)
			}
			for _, want := range c.contains {
				if !strings.Contains(string(out[0].Data), want) {
					t.Errorf("text lacks %q:\n%s", want, out[0].Data)
				}
			}
			for _, unwanted := range c.absent {
				if strings.Contains(string(out[0].Data), unwanted) {
					t.Errorf("text holds %q:\n%s", unwanted, out[0].Data)
				}
			}
		})
	}
}

func TestPrepare_UnpacksAnArchiveOneLevel(t *testing.T) {
	t.Parallel()
	archive := zipOf(t,
		member{name: "docs/", data: nil},
		member{name: "docs/notes.md", data: []byte("# Notes")},
		member{name: "data.csv", data: []byte("a,b\n1,2")},
		member{name: "__MACOSX/docs/._notes.md", data: []byte{0, 5, 22, 7, 0}},
		member{name: ".DS_Store", data: []byte{0, 0, 0, 1}},
	)
	out, err := prepare(t, attach.File{Name: "bundle.zip", Data: archive})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out) != 2 || out[0].Name != "bundle.zip/docs/notes.md" || out[1].Name != "bundle.zip/data.csv" {
		t.Fatalf("members = %+v", out)
	}
}

func TestPrepare_RefusesWhatItShould(t *testing.T) {
	t.Parallel()
	many := make([]member, 21)
	for i := range many {
		many[i] = member{name: fmt.Sprintf("n%d.txt", i), data: []byte("x")}
	}
	cases := []struct {
		label string
		files []attach.File
		want  attach.Reason
	}{
		{"an extension off the list", []attach.File{{Name: "run.exe", Data: []byte("MZ\x90\x00")}}, attach.ReasonType},
		{"no extension", []attach.File{{Name: "README", Data: []byte("text")}}, attach.ReasonType},
		{"an image named as text", []attach.File{{Name: "notes.txt", Data: pngBytes}}, attach.ReasonType},
		{"an archive named as a PDF", []attach.File{{Name: "report.pdf", Data: zipOf(t, member{name: "a.txt", data: []byte("a")})}}, attach.ReasonType},
		{"text named as an image", []attach.File{{Name: "photo.png", Data: []byte("just words")}}, attach.ReasonType},
		{"a plain archive named as slides", []attach.File{{Name: "deck.pptx", Data: zipOf(t, member{name: "a.txt", data: []byte("a")})}}, attach.ReasonType},
		{"a document named as another", []attach.File{{Name: "sheet.xlsx", Data: docx(t)}}, attach.ReasonType},
		{"binary named as csv", []attach.File{{Name: "data.csv", Data: []byte("a,b\x00\x01\x02")}}, attach.ReasonType},
		{"an svg that is not one", []attach.File{{Name: "x.svg", Data: []byte("plain text")}}, attach.ReasonType},
		{"an empty file", []attach.File{{Name: "empty.txt"}}, attach.ReasonEmpty},
		{"a drawing with no words", []attach.File{{Name: "x.svg", Data: []byte(`<svg><rect/></svg>`)}}, attach.ReasonEmpty},
		{"a damaged document", []attach.File{{Name: "a.docx", Data: append([]byte("PK\x03\x04"), make([]byte, 40)...)}}, attach.ReasonUnreadable},
		{"one file too large", []attach.File{{Name: "big.txt", Data: bytes.Repeat([]byte("a"), 5<<20+1)}}, attach.ReasonTooLarge},
		{"too much together", []attach.File{
			{Name: "a.txt", Data: bytes.Repeat([]byte("a"), 4<<20)},
			{Name: "b.txt", Data: bytes.Repeat([]byte("b"), 4<<20)},
			{Name: "c.txt", Data: bytes.Repeat([]byte("c"), 3<<20)},
		}, attach.ReasonTotal},
		{"too many members", []attach.File{{Name: "many.zip", Data: zipOf(t, many...)}}, attach.ReasonCount},
		{"a nested archive", []attach.File{{Name: "outer.zip", Data: zipOf(t,
			member{name: "inner.zip", data: zipOf(t, member{name: "a.txt", data: []byte("a")}), store: true})}}, attach.ReasonNested},
		{"a zip bomb", []attach.File{{Name: "bomb.zip", Data: zipOf(t, member{name: "zeros.txt", data: bytes.Repeat([]byte("0"), 4<<20)})}}, attach.ReasonBomb},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Parallel()
			_, err := prepare(t, c.files...)
			refusal(t, err, c.want)
		})
	}
}

func TestPrepare_RefusesLegacyOfficeNamingTheModernFormat(t *testing.T) {
	t.Parallel()
	ole := append([]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"), make([]byte, 64)...)
	for ext, modern := range map[string]string{"doc": "docx", "ppt": "pptx", "xls": "xlsx"} {
		_, err := prepare(t, attach.File{Name: "old." + ext, Data: ole})
		if e := refusal(t, err, attach.ReasonLegacy); e.Modern != modern || e.Name != "old."+ext {
			t.Errorf("%s: refusal = %+v", ext, e)
		}
	}
}

func TestPrepare_KeepsOnlyTheFileName(t *testing.T) {
	t.Parallel()
	out, err := prepare(t, attach.File{Name: `C:\Users\someone\notes\plan.md`, Data: []byte("plan")})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Name != "plan.md" {
		t.Errorf("name = %q", out[0].Name)
	}
}

type scanner struct {
	malicious bool
	err       error
	seen      [][sha256.Size]byte
}

func (s *scanner) Malicious(_ context.Context, sum [sha256.Size]byte) (bool, error) {
	s.seen = append(s.seen, sum)
	return s.malicious, s.err
}

func TestPrepare_AsksTheScanner(t *testing.T) {
	t.Parallel()
	file := attach.File{Name: "notes.md", Data: []byte("# Notes")}

	flagged := &scanner{malicious: true}
	_, err := attach.Prepare(context.Background(), []attach.File{file}, attach.Options{Scanner: flagged})
	refusal(t, err, attach.ReasonMalicious)
	if flagged.seen[0] != sha256.Sum256(file.Data) {
		t.Error("the scanner was not given the file's hash")
	}

	// A lookup that fails does not block the upload.
	down := &scanner{err: errors.New("timeout")}
	out, err := attach.Prepare(context.Background(), []attach.File{file}, attach.Options{Scanner: down})
	if err != nil || len(out) != 1 {
		t.Errorf("a failed lookup refused the file: %v", err)
	}
}

func TestVirusTotal_SendsOnlyTheHash(t *testing.T) {
	t.Parallel()
	data := []byte("# Notes")
	sum := sha256.Sum256(data)
	var gotPath, gotKey string
	status, body := http.StatusOK, `{"data":{"attributes":{"last_analysis_stats":{"malicious":3,"harmless":60}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("x-apikey")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	vt := &attach.VirusTotal{APIKey: "k", BaseURL: srv.URL}

	malicious, err := vt.Malicious(context.Background(), sum)
	if err != nil || !malicious {
		t.Fatalf("flagged file: malicious = %v, err = %v", malicious, err)
	}
	if gotPath != "/api/v3/files/"+hex.EncodeToString(sum[:]) || gotKey != "k" {
		t.Errorf("request = %s with key %q", gotPath, gotKey)
	}

	status, body = http.StatusNotFound, `{"error":{"code":"NotFoundError"}}`
	if malicious, err := vt.Malicious(context.Background(), sum); err != nil || malicious {
		t.Errorf("unknown file: malicious = %v, err = %v", malicious, err)
	}
	status, body = http.StatusTooManyRequests, `{}`
	if _, err := vt.Malicious(context.Background(), sum); err == nil {
		t.Error("a rate limited lookup reported no error")
	}
}
