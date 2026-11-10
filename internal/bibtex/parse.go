package bibtex

import (
	"fmt"
	"strings"
)

// Entry is one entry of a .bib file. Field names are lower case; a value is
// the text between its delimiters, with its inner braces kept, and the parts
// of a value joined with # are concatenated.
type Entry struct {
	Type   string
	Key    string
	Fields map[string]string
	Line   int
}

// ParseFile parses the entries of a .bib file. @comment and @preamble are
// skipped; @string macros are expanded where they are used. A malformed entry
// yields an error naming its line.
func ParseFile(src string) ([]Entry, error) {
	p := &parser{src: src, macros: map[string]string{}}
	var entries []Entry
	for {
		at := strings.IndexByte(p.src[p.pos:], '@')
		if at < 0 {
			return entries, nil
		}
		p.pos += at + 1
		line := p.line()
		typ := strings.ToLower(p.ident())
		p.space()
		if p.pos >= len(p.src) || (p.src[p.pos] != '{' && p.src[p.pos] != '(') {
			continue // an @ in free text between entries
		}
		closing := byte('}')
		if p.src[p.pos] == '(' {
			closing = ')'
		}
		p.pos++
		switch typ {
		case "comment", "preamble":
			p.pos-- // skip the balanced body
			if _, err := p.braced(p.src[p.pos], closing); err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			continue
		case "string":
			name, value, err := p.field()
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			p.macros[name] = value
			p.space()
			if p.pos < len(p.src) && p.src[p.pos] == closing {
				p.pos++
			}
			continue
		}
		e := Entry{Type: typ, Fields: map[string]string{}, Line: line}
		p.space()
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != ',' && p.src[p.pos] != closing {
			p.pos++
		}
		e.Key = strings.TrimSpace(p.src[start:p.pos])
		for {
			p.space()
			if p.pos >= len(p.src) {
				return nil, fmt.Errorf("line %d: entry %s is not closed", line, e.Key)
			}
			if p.src[p.pos] == closing {
				p.pos++
				break
			}
			if p.src[p.pos] == ',' {
				p.pos++
				continue
			}
			name, value, err := p.field()
			if err != nil {
				return nil, fmt.Errorf("line %d: entry %s: %w", line, e.Key, err)
			}
			e.Fields[name] = value
		}
		entries = append(entries, e)
	}
}

type parser struct {
	src    string
	pos    int
	macros map[string]string
}

func (p *parser) line() int { return strings.Count(p.src[:p.pos], "\n") + 1 }

func (p *parser) space() {
	for p.pos < len(p.src) && strings.IndexByte(" \t\r\n", p.src[p.pos]) >= 0 {
		p.pos++
	}
}

// ident reads a name: letters, digits and the punctuation BibTeX allows.
func (p *parser) ident() string {
	start := p.pos
	for p.pos < len(p.src) && strings.IndexByte(" \t\r\n{}(),=#\"", p.src[p.pos]) < 0 {
		p.pos++
	}
	return p.src[start:p.pos]
}

// field reads name = value [# value]...
func (p *parser) field() (string, string, error) {
	p.space()
	name := strings.ToLower(p.ident())
	if name == "" {
		return "", "", fmt.Errorf("expected a field name")
	}
	p.space()
	if p.pos >= len(p.src) || p.src[p.pos] != '=' {
		return "", "", fmt.Errorf("field %s has no =", name)
	}
	p.pos++
	var value strings.Builder
	for {
		p.space()
		if p.pos >= len(p.src) {
			return "", "", fmt.Errorf("field %s has no value", name)
		}
		switch c := p.src[p.pos]; c {
		case '{':
			part, err := p.braced('{', '}')
			if err != nil {
				return "", "", err
			}
			value.WriteString(part)
		case '"':
			part, err := p.braced('"', '"')
			if err != nil {
				return "", "", err
			}
			value.WriteString(part)
		default:
			word := p.ident()
			if word == "" {
				return "", "", fmt.Errorf("field %s has no value", name)
			}
			if m, ok := p.macros[strings.ToLower(word)]; ok {
				word = m
			}
			value.WriteString(word)
		}
		p.space()
		if p.pos < len(p.src) && p.src[p.pos] == '#' {
			p.pos++
			continue
		}
		return name, value.String(), nil
	}
}

// braced reads a value delimited by open and closing, with nested braces,
// and returns its content.
func (p *parser) braced(open, closing byte) (string, error) {
	start := p.pos + 1
	depth := 0
	for i := start; i < len(p.src); i++ {
		switch c := p.src[i]; {
		case c == '\\':
			i++ // an escaped character never delimits
		case c == '{':
			depth++
		case c == '}' && depth > 0:
			depth--
		case c == closing && depth == 0:
			p.pos = i + 1
			return p.src[start:i], nil
		}
	}
	return "", fmt.Errorf("unbalanced %c", open)
}
