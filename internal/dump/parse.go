package dump

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse streams the dump and invokes fn for every publication record.
// It works on either the gzip-compressed or plain XML file (detected by the
// .gz extension) and keeps memory flat by reading one record at a time.
//
// dblp.xml has a simple structure: records contain fields, and a field
// contains text with at most some inline markup, such as <i> and <sub> in
// titles. Parse scans for that structure directly, which is several times
// faster than encoding/xml. The text of inline markup is kept: "<i>O</i>-Notation."
// becomes "O-Notation.". Entities are decoded with xml.HTMLEntity, so the
// private dblp.dtd is never needed, and text is converted from ISO-8859-1
// when the XML declaration names that encoding.
func Parse(path string, fn func(Record) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}

	s := &scanner{r: bufio.NewReaderSize(r, 1<<20)}
	for {
		name, attrs, err := s.nextStart()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !RecordTypes[name] {
			continue
		}
		body, err := s.until("</" + name + ">")
		if err != nil {
			return err
		}
		rec, err := s.record(name, attrs, body)
		if err != nil {
			// Skip a malformed record rather than aborting the whole dump.
			continue
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
}

type scanner struct {
	r      *bufio.Reader
	tag    []byte
	body   []byte
	latin1 bool // the XML declaration names ISO-8859-1
}

// nextStart skips to the next start tag and returns its name and its raw
// attribute text. End tags, comments and the DOCTYPE are skipped; the XML
// declaration sets the encoding.
func (s *scanner) nextStart() (string, []byte, error) {
	for {
		if err := s.skipTo('<'); err != nil {
			return "", nil, err
		}
		tag, err := s.readTo('>', s.tag[:0])
		if err != nil {
			return "", nil, err
		}
		s.tag = tag
		tag = tag[:len(tag)-1]
		if len(tag) == 0 || tag[0] == '/' || tag[0] == '!' {
			continue
		}
		if tag[0] == '?' {
			if bytes.HasPrefix(tag, []byte("?xml")) {
				s.latin1 = latin1Label(attr(tag, "encoding", false))
			}
			continue
		}
		end := bytes.IndexAny(tag, " \t\r\n/")
		if end < 0 {
			end = len(tag)
		}
		return string(tag[:end]), tag[end:], nil
	}
}

// skipTo discards input up to and including the byte c.
func (s *scanner) skipTo(c byte) error {
	for {
		_, err := s.r.ReadSlice(c)
		if err != bufio.ErrBufferFull {
			return err
		}
	}
}

// readTo appends input up to and including the byte c to out.
func (s *scanner) readTo(c byte, out []byte) ([]byte, error) {
	for {
		chunk, err := s.r.ReadSlice(c)
		out = append(out, chunk...)
		if err != bufio.ErrBufferFull {
			return out, err
		}
	}
}

// until returns the input up to the closing tag and consumes the tag. The
// returned slice is valid until the next call.
func (s *scanner) until(closing string) ([]byte, error) {
	out := s.body[:0]
	for {
		var err error
		out, err = s.readTo(closing[len(closing)-1], out)
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
		if bytes.HasSuffix(out, []byte(closing)) {
			s.body = out
			return out[:len(out)-len(closing)], nil
		}
	}
}

var errMalformed = errors.New("malformed record")

// record builds a Record from the attributes and the content of a record
// element.
func (s *scanner) record(typ string, attrs, body []byte) (Record, error) {
	rec := Record{
		Type:  typ,
		Key:   attr(attrs, "key", s.latin1),
		MDate: attr(attrs, "mdate", s.latin1),
	}
	for {
		i := bytes.IndexByte(body, '<')
		if i < 0 {
			break
		}
		body = body[i+1:]
		j := bytes.IndexByte(body, '>')
		if j < 0 {
			return rec, errMalformed
		}
		tag := body[:j]
		body = body[j+1:]
		if len(tag) == 0 || tag[0] == '/' || tag[0] == '!' || tag[0] == '?' {
			continue
		}
		selfClosing := tag[len(tag)-1] == '/'
		end := bytes.IndexAny(tag, " \t\r\n/")
		if end < 0 {
			end = len(tag)
		}
		name := string(tag[:end])
		value := ""
		if !selfClosing {
			closing := "</" + name + ">"
			k := bytes.Index(body, []byte(closing))
			if k < 0 {
				return rec, errMalformed
			}
			value = text(body[:k], s.latin1)
			body = body[k+len(closing):]
		}
		switch name {
		case "author":
			rec.Authors = append(rec.Authors, value)
		case "editor":
			rec.Editors = append(rec.Editors, value)
		case "ee":
			rec.EE = append(rec.EE, value)
		case "title":
			rec.Title = strings.TrimSpace(value)
		case "year":
			rec.Year = value
		case "month":
			rec.Month = value
		case "journal":
			rec.Journal = value
		case "booktitle":
			rec.Booktitle = value
		case "pages":
			rec.Pages = value
		case "volume":
			rec.Volume = value
		case "number":
			rec.Number = value
		case "publisher":
			rec.Publisher = value
		case "school":
			rec.School = value
		case "series":
			rec.Series = value
		case "isbn":
			rec.ISBN = value
		case "url":
			rec.URL = value
		}
	}
	return rec, nil
}

// attr returns the decoded value of the named attribute in raw tag text.
func attr(attrs []byte, name string, latin1 bool) string {
	for _, quote := range []string{`"`, `'`} {
		prefix := []byte(name + "=" + quote)
		rest := attrs
		for {
			i := bytes.Index(rest, prefix)
			if i < 0 {
				break
			}
			if i > 0 && !isSpace(rest[i-1]) {
				rest = rest[i+len(prefix):]
				continue
			}
			v := rest[i+len(prefix):]
			if j := bytes.IndexByte(v, quote[0]); j >= 0 {
				return text(v[:j], latin1)
			}
			return ""
		}
	}
	return ""
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// text returns the character data of raw element content: tags are removed
// and their text kept, entities are decoded, and Latin-1 bytes are converted
// to UTF-8 when latin1 is set. An unknown entity is kept as written.
func text(raw []byte, latin1 bool) string {
	if !bytes.ContainsAny(raw, "<&\r") && (!latin1 || isASCII(raw)) {
		return string(raw)
	}
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '<':
			if j := bytes.IndexByte(raw[i:], '>'); j >= 0 {
				i += j
				continue
			}
			b.WriteByte(c)
		case c == '&':
			if j := bytes.IndexByte(raw[i:], ';'); j > 0 {
				if s, ok := entity(string(raw[i+1 : i+j])); ok {
					b.WriteString(s)
					i += j
					continue
				}
			}
			b.WriteByte(c)
		case c == '\r':
			// XML normalises CR LF and lone CR to LF.
			b.WriteByte('\n')
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
		case c >= utf8.RuneSelf && latin1:
			b.WriteRune(rune(c))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

var xmlEntity = map[string]string{"lt": "<", "gt": ">", "amp": "&", "apos": "'", "quot": `"`}

// entity returns the text of a predefined XML, HTML or numeric entity.
func entity(name string) (string, bool) {
	if s, ok := xmlEntity[name]; ok {
		return s, true
	}
	if s, ok := xml.HTMLEntity[name]; ok {
		return s, true
	}
	if strings.HasPrefix(name, "#") {
		var n uint64
		var err error
		if strings.HasPrefix(name, "#x") {
			n, err = strconv.ParseUint(name[2:], 16, 32)
		} else {
			n, err = strconv.ParseUint(name[1:], 10, 32)
		}
		if err == nil && utf8.ValidRune(rune(n)) {
			return string(rune(n)), true
		}
	}
	return "", false
}

// latin1Label reports whether an encoding label names ISO-8859-1 or one of
// its aliases. dblp content is effectively ASCII with entities; windows-1252
// is read as Latin-1, which differs only in the unused range 0x80..0x9F.
func latin1Label(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return true
	}
	return false
}
