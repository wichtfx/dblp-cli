package dump

import (
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"strings"
)

// Parse streams the dump and invokes fn for every publication record.
// It works on either the gzip-compressed or plain XML file (detected by the
// .gz extension) and keeps memory flat by decoding one record at a time.
//
// dblp.xml uses HTML named entities (&uuml;, &eacute;, ...) defined in its
// private DTD. We map those via xml.HTMLEntity and disable strict mode so
// the parser never needs the external dblp.dtd file.
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

	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = charsetReader

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || !RecordTypes[se.Name.Local] {
			continue
		}
		var rec Record
		if err := dec.DecodeElement(&rec, &se); err != nil {
			// Skip a malformed record rather than aborting the whole dump.
			continue
		}
		rec.Type = se.Name.Local
		rec.Title = strings.TrimSpace(rec.Title)
		if err := fn(rec); err != nil {
			return err
		}
	}
}

// charsetReader lets the decoder accept dblp.xml's declared ISO-8859-1
// encoding (and a few aliases). dblp content is effectively ASCII with HTML
// entities, but any high bytes are decoded as Latin-1. Unknown labels are
// passed through unchanged as a best effort.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return &latin1Reader{r: input}, nil
	default: // "", utf-8, us-ascii, ...
		return input, nil
	}
}

// latin1Reader decodes Latin-1 bytes into UTF-8 on the fly.
type latin1Reader struct {
	r   io.Reader
	buf [4096]byte
	out [8192]byte // each Latin-1 byte becomes at most two UTF-8 bytes
	pnd []byte     // pending UTF-8 bytes not yet delivered
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(l.pnd) == 0 {
		n, err := l.r.Read(l.buf[:])
		if n == 0 {
			return 0, err
		}
		out := l.out[:0]
		for _, b := range l.buf[:n] {
			if b < 0x80 {
				out = append(out, b)
			} else { // 0x80..0xFF -> two-byte UTF-8
				out = append(out, 0xC0|b>>6, 0x80|b&0x3F)
			}
		}
		l.pnd = out
	}
	n := copy(p, l.pnd)
	l.pnd = l.pnd[n:]
	return n, nil
}
