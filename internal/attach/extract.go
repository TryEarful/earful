package attach

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// The Office Open XML and OpenDocument formats are zip containers of
// XML parts, read here with the standard library. Spreadsheets are read
// the same way rather than through the spreadsheet library the export
// uses: that library moves large parts of a workbook it opens into
// temporary files, and an upload must never reach the disk.

// textOut collects extracted text up to a limit, past which the
// document is refused rather than quietly cut short.
type textOut struct {
	b   strings.Builder
	max int64
}

func (t *textOut) write(s string) error {
	if int64(t.b.Len()+len(s)) > t.max {
		return &Error{Reason: ReasonTooLarge}
	}
	t.b.WriteString(s)
	return nil
}

// documentText reads the text of an Office or OpenDocument file.
func documentText(k kind, data []byte, lim Limits) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", &Error{Reason: ReasonUnreadable}
	}
	b := &budget{left: lim.MaxDocumentXMLBytes}
	out := &textOut{max: lim.MaxFileBytes}
	switch k {
	case kindDOCX:
		err = readPart(r, b, "word/document.xml", docxRules, out)
	case kindPPTX:
		err = pptxText(r, b, out)
	case kindXLSX:
		err = xlsxText(r, b, out, lim.MaxFileBytes)
	case kindODT, kindODP:
		err = readPart(r, b, "content.xml", odfRules, out)
	default:
		err = &Error{Reason: ReasonType}
	}
	if err != nil {
		return "", err
	}
	return tidy(out.b.String()), nil
}

// xmlRules say how to read text out of one XML vocabulary, by local
// element name (namespaces are ignored).
type xmlRules struct {
	// collect: character data inside any of these is text.
	collect map[string]bool
	// skip: subtrees ignored entirely.
	skip map[string]bool
	// open and close: what to write when an element starts or ends.
	open, close map[string]string
}

var (
	// WordprocessingML: runs of text are w:t, a paragraph is w:p. Tab
	// stops in paragraph properties are also called tab, hence skipping
	// pPr.
	docxRules = xmlRules{
		collect: map[string]bool{"t": true},
		skip:    map[string]bool{"pPr": true},
		open:    map[string]string{"tab": "\t", "br": "\n", "cr": "\n"},
		close:   map[string]string{"p": "\n"},
	}
	// DrawingML, as slides use it: a:t inside a:p.
	pptxRules = xmlRules{
		collect: map[string]bool{"t": true},
		open:    map[string]string{"br": "\n"},
		close:   map[string]string{"p": "\n"},
	}
	// OpenDocument: text:p and text:h hold the text, with text:s for
	// runs of spaces.
	odfRules = xmlRules{
		collect: map[string]bool{"p": true, "h": true},
		skip:    map[string]bool{"automatic-styles": true, "styles": true},
		open:    map[string]string{"s": " ", "tab": "\t", "line-break": "\n"},
		close:   map[string]string{"p": "\n", "h": "\n"},
	}
	// SVG: the words a drawing carries. Script and style are code.
	svgRules = xmlRules{
		collect: map[string]bool{"text": true, "title": true, "desc": true},
		skip:    map[string]bool{"script": true, "style": true},
		close:   map[string]string{"text": "\n", "title": "\n", "desc": "\n"},
	}
)

// readPart extracts the text of one named part.
func readPart(r *zip.Reader, b *budget, name string, rules xmlRules, out *textOut) error {
	f := findPart(r, name)
	if f == nil {
		return &Error{Reason: ReasonUnreadable}
	}
	rc, err := b.open(f)
	if err != nil {
		return err
	}
	defer rc.Close()
	return xmlText(newDecoder(rc, true), rules, out)
}

func newDecoder(r io.Reader, strict bool) *xml.Decoder {
	d := xml.NewDecoder(r)
	if !strict {
		d.Strict = false
		d.AutoClose = xml.HTMLAutoClose
		d.Entity = xml.HTMLEntity
	}
	return d
}

// xmlText walks a document and writes its text by rules. encoding/xml
// neither fetches external entities nor expands custom ones, so a
// hostile document cannot reach out or multiply itself.
func xmlText(d *xml.Decoder, rules xmlRules, out *textOut) error {
	collecting, skipping := 0, 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return decodeError(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if skipping > 0 || rules.skip[name] {
				skipping++
				continue
			}
			if rules.collect[name] {
				collecting++
			}
			if s, ok := rules.open[name]; ok {
				if err := out.write(s); err != nil {
					return err
				}
			}
		case xml.EndElement:
			name := t.Name.Local
			if skipping > 0 {
				skipping--
				continue
			}
			if rules.collect[name] && collecting > 0 {
				collecting--
			}
			if s, ok := rules.close[name]; ok {
				if err := out.write(s); err != nil {
					return err
				}
			}
		case xml.CharData:
			if collecting > 0 && skipping == 0 {
				if err := out.write(string(t)); err != nil {
					return err
				}
			}
		}
	}
}

// decodeError keeps a refusal raised underneath the decoder (the
// budget, the size cap) and calls anything else unreadable.
func decodeError(err error) error {
	var refusal *Error
	if errors.As(err, &refusal) {
		return refusal
	}
	if errors.Is(err, errBudget) {
		return &Error{Reason: ReasonBomb}
	}
	return &Error{Reason: ReasonUnreadable}
}

var slidePart = regexp.MustCompile(`(?i)^ppt/slides/slide(\d+)\.xml$`)

// pptxText reads the slides in their numbered order.
func pptxText(r *zip.Reader, b *budget, out *textOut) error {
	type slide struct {
		n int
		f *zip.File
	}
	var slides []slide
	for _, f := range r.File {
		if m := slidePart.FindStringSubmatch(f.Name); m != nil {
			n, _ := strconv.Atoi(m[1])
			slides = append(slides, slide{n, f})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	for _, s := range slides {
		rc, err := b.open(s.f)
		if err != nil {
			return err
		}
		err = xmlText(newDecoder(rc, true), pptxRules, out)
		rc.Close()
		if err != nil {
			return err
		}
		if err := out.write("\n"); err != nil {
			return err
		}
	}
	return nil
}

// xlsxText writes each sheet under its name, one row per line and the
// cells of a row separated by tabs: the shape a model reads a table in,
// and the shape "a spreadsheet of questions and answers" needs to keep.
func xlsxText(r *zip.Reader, b *budget, out *textOut, maxStrings int64) error {
	shared, err := sharedStrings(r, b, maxStrings)
	if err != nil {
		return err
	}
	sheets, err := workbookSheets(r, b)
	if err != nil {
		return err
	}
	for _, sheet := range sheets {
		f := findPart(r, sheet.part)
		if f == nil {
			continue
		}
		if err := out.write("Sheet: " + sheet.name + "\n"); err != nil {
			return err
		}
		rc, err := b.open(f)
		if err != nil {
			return err
		}
		err = worksheetText(newDecoder(rc, true), shared, out)
		rc.Close()
		if err != nil {
			return err
		}
		if err := out.write("\n"); err != nil {
			return err
		}
	}
	return nil
}

// sharedStrings reads the workbook's string table, where most cell
// text lives. Phonetic guides (rPh) repeat a string's reading and are
// left out.
func sharedStrings(r *zip.Reader, b *budget, max int64) ([]string, error) {
	f := findPart(r, "xl/sharedStrings.xml")
	if f == nil {
		return nil, nil
	}
	rc, err := b.open(f)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	d := newDecoder(rc, true)
	var (
		table    []string
		current  strings.Builder
		inT      bool
		phonetic int
		size     int64
	)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return table, nil
		}
		if err != nil {
			return nil, decodeError(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				current.Reset()
			case "t":
				inT = true
			case "rPh":
				phonetic++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				size += int64(current.Len())
				if size > max {
					return nil, &Error{Reason: ReasonTooLarge}
				}
				table = append(table, current.String())
			case "t":
				inT = false
			case "rPh":
				phonetic--
			}
		case xml.CharData:
			if inT && phonetic == 0 {
				current.Write(t)
			}
		}
	}
}

type sheetRef struct{ name, part string }

// workbookSheets lists the sheets in the order the workbook shows them,
// each with the part that holds it, resolved through the workbook's
// relationships.
func workbookSheets(r *zip.Reader, b *budget) ([]sheetRef, error) {
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := decodePart(r, b, "xl/workbook.xml", &workbook); err != nil {
		return nil, err
	}
	var rels struct {
		Relationships []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := decodePart(r, b, "xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	for _, rel := range rels.Relationships {
		target := rel.Target
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(path.Clean(target), "/")
		} else {
			target = path.Clean("xl/" + target)
		}
		targets[rel.ID] = target
	}
	var sheets []sheetRef
	for _, s := range workbook.Sheets {
		if part, ok := targets[s.ID]; ok {
			sheets = append(sheets, sheetRef{name: s.Name, part: part})
		}
	}
	return sheets, nil
}

func decodePart(r *zip.Reader, b *budget, name string, v any) error {
	f := findPart(r, name)
	if f == nil {
		return &Error{Reason: ReasonUnreadable}
	}
	rc, err := b.open(f)
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := xml.NewDecoder(rc).Decode(v); err != nil {
		return decodeError(err)
	}
	return nil
}

// worksheetText writes one sheet's rows. A cell's value is in v, as an
// index into the string table when its type is "s"; an inline string
// is in is/t. Formulas (f) are not text a reader sees.
func worksheetText(d *xml.Decoder, shared []string, out *textOut) error {
	var (
		row      []string
		cell     strings.Builder
		cellType string
		inValue  bool
	)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return decodeError(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = row[:0]
			case "c":
				cell.Reset()
				cellType = ""
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			case "v", "t":
				inValue = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inValue = false
			case "c":
				value := cell.String()
				if cellType == "s" {
					if i, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && i >= 0 && i < len(shared) {
						value = shared[i]
					}
				}
				row = append(row, strings.TrimSpace(value))
			case "row":
				line := strings.TrimRight(strings.Join(row, "\t"), "\t")
				if line != "" {
					if err := out.write(line + "\n"); err != nil {
						return err
					}
				}
			}
		case xml.CharData:
			if inValue {
				if int64(cell.Len()+len(t)) > out.max {
					return &Error{Reason: ReasonTooLarge}
				}
				cell.Write(t)
			}
		}
	}
}

// svgText reads the words of a drawing. It is parsed leniently, since
// SVG written by hand is often not well formed.
func svgText(data []byte, lim Limits) (string, error) {
	out := &textOut{max: lim.MaxFileBytes}
	if err := xmlText(newDecoder(bytes.NewReader(data), false), svgRules, out); err != nil {
		return "", err
	}
	return tidy(out.b.String()), nil
}

// htmlBlocks end a line of text where they start or end.
var htmlBlocks = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "table": true, "ul": true, "ol": true,
	"blockquote": true, "pre": true, "dt": true, "dd": true, "title": true,
}

// htmlHidden are elements whose content is not text a reader sees.
var htmlHidden = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
}

// htmlText reads the text a browser would show, one block per line.
func htmlText(data []byte, lim Limits) (string, error) {
	out := &textOut{max: lim.MaxFileBytes}
	z := html.NewTokenizer(bytes.NewReader(data))
	hidden := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return tidy(out.b.String()), nil
			}
			return "", &Error{Reason: ReasonUnreadable}
		case html.StartTagToken:
			name, _ := z.TagName()
			if htmlHidden[string(name)] {
				hidden++
			}
			if err := htmlBreak(out, string(name)); err != nil {
				return "", err
			}
		case html.SelfClosingTagToken:
			name, _ := z.TagName()
			if err := htmlBreak(out, string(name)); err != nil {
				return "", err
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if htmlHidden[string(name)] && hidden > 0 {
				hidden--
			}
			if err := htmlBreak(out, string(name)); err != nil {
				return "", err
			}
		case html.TextToken:
			// Text comes back with its entities already decoded.
			if hidden == 0 {
				if err := out.write(string(z.Text())); err != nil {
					return "", err
				}
			}
		}
	}
}

func htmlBreak(out *textOut, tag string) error {
	if htmlBlocks[tag] {
		return out.write("\n")
	}
	return nil
}

// tidy collapses runs of spaces in each line and runs of blank lines,
// which markup leaves behind in quantity.
func tidy(s string) string {
	var lines []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		cells := strings.Split(line, "\t")
		for i, c := range cells {
			cells[i] = strings.Join(strings.Fields(c), " ")
		}
		line = strings.TrimRight(strings.Join(cells, "\t"), "\t")
		if strings.TrimSpace(line) == "" {
			if !blank && len(lines) > 0 {
				lines = append(lines, "")
			}
			blank = true
			continue
		}
		blank = false
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
