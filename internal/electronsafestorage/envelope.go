package electronsafestorage

import (
	"bytes"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// decodeEnvelope is intentionally a small strict JSON decoder: it rejects
// unknown/duplicate/missing fields while keeping apiKey in mutable storage.
func decodeEnvelope(raw []byte) (apiKey []byte, origin, protocol string, err error) {
	p := envelopeParser{raw: raw}
	defer func() {
		if err != nil {
			zero(apiKey)
			apiKey = nil
		}
	}()
	if !p.take('{') {
		return nil, "", "", ErrInvalidInput
	}
	seen := map[string]bool{}
	version := int64(-1)
	for {
		p.space()
		if p.take('}') {
			break
		}
		nameBytes, e := p.stringBytes(32)
		if e != nil {
			return nil, "", "", e
		}
		name := string(nameBytes)
		zero(nameBytes)
		if seen[name] {
			return nil, "", "", ErrInvalidInput
		}
		seen[name] = true
		if !p.take(':') {
			return nil, "", "", ErrInvalidInput
		}
		switch name {
		case "version":
			version, e = p.integer()
		case "apiKey":
			apiKey, e = p.stringBytes(maxAPIKey)
		case "origin":
			var b []byte
			b, e = p.stringBytes(2048)
			origin = string(b)
			zero(b)
		case "protocol":
			var b []byte
			b, e = p.stringBytes(64)
			protocol = string(b)
			zero(b)
		default:
			return nil, "", "", ErrInvalidInput
		}
		if e != nil {
			return nil, "", "", e
		}
		p.space()
		if p.take('}') {
			break
		}
		if !p.take(',') {
			return nil, "", "", ErrInvalidInput
		}
	}
	p.space()
	if p.i != len(raw) || len(seen) != 4 || version != 1 || len(apiKey) == 0 || len(apiKey) > maxAPIKey || origin == "" || protocol == "" {
		return nil, "", "", ErrInvalidInput
	}
	return apiKey, origin, protocol, nil
}

type envelopeParser struct {
	raw []byte
	i   int
}

func (p *envelopeParser) space() {
	for p.i < len(p.raw) && bytes.IndexByte([]byte(" \t\r\n"), p.raw[p.i]) >= 0 {
		p.i++
	}
}
func (p *envelopeParser) take(c byte) bool {
	p.space()
	if p.i >= len(p.raw) || p.raw[p.i] != c {
		return false
	}
	p.i++
	return true
}
func (p *envelopeParser) integer() (int64, error) {
	p.space()
	start := p.i
	for p.i < len(p.raw) && p.raw[p.i] >= '0' && p.raw[p.i] <= '9' {
		p.i++
	}
	if start == p.i {
		return 0, ErrInvalidInput
	}
	if p.i-start > 1 && p.raw[start] == '0' {
		return 0, ErrInvalidInput
	}
	v, err := strconv.ParseInt(string(p.raw[start:p.i]), 10, 64)
	return v, err
}
func (p *envelopeParser) stringBytes(limit int) ([]byte, error) {
	p.space()
	if p.i >= len(p.raw) || p.raw[p.i] != '"' {
		return nil, ErrInvalidInput
	}
	p.i++
	out := make([]byte, 0, 64)
	fail := func() ([]byte, error) { zero(out); return nil, ErrInvalidInput }
	for p.i < len(p.raw) {
		c := p.raw[p.i]
		p.i++
		if c == '"' {
			if len(out) > limit || !utf8.Valid(out) {
				return fail()
			}
			return out, nil
		}
		if c < 0x20 {
			return fail()
		}
		if c != '\\' {
			out = append(out, c)
		} else {
			if p.i >= len(p.raw) {
				return fail()
			}
			e := p.raw[p.i]
			p.i++
			switch e {
			case '"', '\\', '/':
				out = append(out, e)
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				r, ok := p.hexRune()
				if !ok {
					return fail()
				}
				if utf16.IsSurrogate(r) {
					if p.i+2 > len(p.raw) || p.raw[p.i] != '\\' || p.raw[p.i+1] != 'u' {
						return fail()
					}
					p.i += 2
					r2, ok := p.hexRune()
					if !ok {
						return fail()
					}
					r = utf16.DecodeRune(r, r2)
					if r == utf8.RuneError {
						return fail()
					}
				}
				out = utf8.AppendRune(out, r)
			default:
				return fail()
			}
		}
		if len(out) > limit {
			return fail()
		}
	}
	return fail()
}
func (p *envelopeParser) hexRune() (rune, bool) {
	if p.i+4 > len(p.raw) {
		return 0, false
	}
	v, err := strconv.ParseUint(string(p.raw[p.i:p.i+4]), 16, 16)
	p.i += 4
	return rune(v), err == nil
}
