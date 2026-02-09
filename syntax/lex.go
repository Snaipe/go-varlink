// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package syntax

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Cursor represents a cursor position within a document, i.e. a line and
// a column number, both starting at 1.
type Cursor struct {
	Line, Column int
}

// Token represents a token in the lexer stream.
type Token struct {
	// The type of this token.
	Type TokenType

	// The original string representation of this token.
	Raw string

	// The value interpreted from Raw (may be nil).
	Value interface{}

	// The starting position of this token.
	Start Cursor

	// The end position of this token.
	End Cursor
}

// IsAny returns true if the token is one of the specified token types.
func (tok *Token) IsAny(types ...TokenType) bool {
	for _, typ := range types {
		if tok.Type == typ {
			return true
		}
	}
	return false
}

// TokenType is the type of a token.
type TokenType string

const (
	TokenEOF           TokenType = ""
	TokenError         TokenType = "<error>"
	TokenNewline       TokenType = "<newline>"
	TokenWhitespace    TokenType = "<whitespace>"
	TokenComment       TokenType = "<comment>"
	TokenName          TokenType = "<name>"
	TokenFieldName     TokenType = "<field-name>"
	TokenInterfaceName TokenType = "<interface-name>"
	TokenTypeDef       TokenType = "type"
	TokenInterfaceDef  TokenType = "interface"
	TokenMethodDef     TokenType = "method"
	TokenErrorDef      TokenType = "error"
	TokenArray         TokenType = "[]"
	TokenDict          TokenType = "[string]"
	TokenOption        TokenType = "?"
	TokenLParen        TokenType = "("
	TokenRParen        TokenType = ")"
	TokenColon         TokenType = ":"
	TokenComma         TokenType = ","
	TokenArrow         TokenType = "->"
	TokenTypeBool      TokenType = "bool"
	TokenTypeString    TokenType = "string"
	TokenTypeAny       TokenType = "any"
	TokenTypeObject    TokenType = "object"
	TokenTypeInt       TokenType = "int"
	TokenTypeFloat     TokenType = "float"
)

func (typ TokenType) String() string {
	if typ == TokenEOF {
		return "<eof>"
	}
	return string(typ)
}

type backbuffer struct {
	buf [2]struct {
		r    rune
		w    int
		next Cursor
		pos  Cursor
	}
	ridx int
	rlen int
	widx int
}

func (b *backbuffer) inc(i, inc int) int {
	i = (i + inc) % len(b.buf)
	if i < 0 {
		i += len(b.buf)
	}
	return i
}

func (b *backbuffer) cap() int {
	return cap(b.buf)
}

func (b *backbuffer) write(r rune, w int, next, pos Cursor) {
	if b.rlen != 0 {
		panic("programming error: can't write into backbuffer while there are unread runes")
	}
	e := &b.buf[b.widx]
	e.r, e.w, e.next, e.pos = r, w, next, pos
	b.widx = b.inc(b.widx, 1)
}

func (b *backbuffer) read() (rune, int, Cursor, Cursor) {
	if b.rlen == 0 {
		panic("programming error: no runes in backbuffer")
	}
	e := &b.buf[b.inc(b.widx, -b.rlen)]
	b.rlen--
	return e.r, e.w, e.next, e.pos
}

func (b *backbuffer) unread() (rune, int, Cursor, Cursor) {
	if b.rlen >= len(b.buf) {
		panic("programming error: can't unread more bytes than backbuffer capacity")
	}
	b.rlen++
	ret := &b.buf[b.inc(b.widx, -b.rlen)]
	if ret.w == 0 {
		panic("programming error: can't unread more bytes than backbuffer length")
	}
	return ret.r, ret.w, ret.next, ret.pos
}

type stateFunc func() stateFunc

// Lexer lexes the Varlink IDL Input to produce Tokens.
type Lexer struct {
	// The input of this lexer. Typically a bufio.Reader.
	Input io.RuneReader

	// The cursor position marking the start of the current token.
	TokenPosition Cursor

	// The cursor position at which the lexer will be reading next.
	NextPosition Cursor

	// The cursor position of the current rune.
	Position Cursor

	// The token type to coerce identifiers to.
	CoerceIdentifierType TokenType

	state  stateFunc    // current state
	token  bytes.Buffer // current token
	tokens chan Token   // token ring buffer
	prev   backbuffer   // stashed runes for UnreadRune
	unread int          // number of unread bytes
}

// NewLexer creates a new lexer using the input Reader as source.
func NewLexer(input io.Reader) *Lexer {
	rscan, ok := input.(io.RuneReader)
	if !ok {
		rscan = bufio.NewReader(input)
	}

	l := Lexer{
		Input: rscan,
	}
	l.reset()
	return &l
}

func (l *Lexer) reset() {
	l.state = l.lex
	l.NextPosition = Cursor{1, 1}
	l.Position = l.NextPosition
	l.TokenPosition = l.NextPosition
	l.tokens = make(chan Token, 2)
	l.token.Reset()
}

// Next advances the lexer stream and returns the next token.
func (l *Lexer) Next() Token {
	for {
		select {
		case token := <-l.tokens:
			return token
		default:
			l.state = l.state()
		}
	}
}

func (l *Lexer) error(err error) stateFunc {
	typ := TokenError
	if err == io.EOF {
		typ = TokenEOF
	}
	if _, ok := err.(*Error); !ok {
		err = &Error{Cursor: l.TokenPosition, Err: err}
	}
	token := Token{
		Type:  typ,
		Value: err,
		Raw:   l.tokenText(),
		Start: l.TokenPosition,
		End:   l.Position,
	}
	l.tokens <- token
	l.TokenPosition = l.NextPosition
	close(l.tokens)
	return nil
}

func (l *Lexer) errorf(format string, args ...interface{}) stateFunc {
	return l.error(fmt.Errorf(format, args...))
}

func (l *Lexer) emit(typ TokenType, val interface{}) {
	token := Token{
		Type:  typ,
		Raw:   l.tokenText(),
		Value: val,
		Start: l.TokenPosition,
		End:   l.Position,
	}
	l.token.Reset()
	l.tokens <- token
	l.TokenPosition = l.NextPosition
}

func (l *Lexer) discard() {
	l.token.Reset()
	l.TokenPosition = l.NextPosition
}

func (l *Lexer) readRune() (r rune, w int, err error) {
	if l.unread > 0 {
		r, w, l.NextPosition, l.Position = l.prev.read()
		l.unread--
	} else {
		r, w, err = l.Input.ReadRune()
		if err != nil {
			return 0, 0, err
		}
		if r == utf8.RuneError {
			return 0, 0, fmt.Errorf("bad UTF-8 character")
		}
		l.prev.write(r, w, l.NextPosition, l.Position)
	}
	l.token.WriteRune(r)
	l.Position = l.NextPosition
	switch r {
	case '\n':
		l.NextPosition.Line++
		l.NextPosition.Column = 1
	default:
		l.NextPosition.Column++
	}
	return r, w, nil
}

func (l *Lexer) unreadRune() error {
	_, w, next, pos := l.prev.unread()
	l.unread++
	l.NextPosition = next
	l.Position = pos
	l.token.Truncate(l.token.Len() - w)
	return nil
}

func (l *Lexer) peekRune() (rune, int, error) {
	r, w, err := l.readRune()
	if err != nil {
		return 0, 0, err
	}
	l.unreadRune()
	return r, w, nil
}

func (l *Lexer) tokenText() string {
	return string(l.token.Bytes())
}

func (l *Lexer) acceptRune(exp rune) (rune, error) {
	r, _, err := l.readRune()
	switch {
	case err != nil:
		return 0, err
	case r != exp:
		return r, fmt.Errorf("expected character %q, got %q", exp, r)
	}
	return r, nil
}

func (l *Lexer) acceptString(exp string) (string, error) {
	for _, rexp := range exp {
		r, _, err := l.readRune()
		switch {
		case err != nil:
			return "", err
		case r != rexp:
			return "", fmt.Errorf("unexpected character %q in expected string %q", r, exp)
		}
	}
	return exp, nil
}

func (l *Lexer) acceptUntil(fn func(rune) bool) (string, error) {
	var out strings.Builder
	for {
		r, _, err := l.readRune()
		if err == io.EOF {
			return out.String(), nil
		}
		if err != nil {
			return "", err
		}
		if !fn(r) {
			l.unreadRune()
			return out.String(), nil
		}
		out.WriteRune(r)
	}
}

func (l *Lexer) acceptNewline() error {
	r, _, err := l.readRune()
	switch {
	case err != nil:
		return err
	case r == '\n':
		return nil
	case r == '\r':
		_, err = l.acceptRune('\n')
		return err
	}
	return fmt.Errorf("expected '\\n' or '\\r', got %q", r)
}

const (
	lineSep = '\u2028'
	parSep  = '\u2029'
)

func (l *Lexer) lex() stateFunc {
	r, _, err := l.readRune()
	if err != nil {
		return l.error(err)
	}

	switch r {
	case '(':
		l.emit(TokenLParen, nil)
	case ')':
		l.emit(TokenRParen, nil)
	case '[':
		r, _, err := l.readRune()
		if err != nil {
			return l.error(err)
		}
		switch r {
		case ']':
			l.emit(TokenArray, nil)
		case 's':
			if _, err := l.acceptString("tring]"); err != nil {
				return l.errorf("expected [string]")
			}
			l.emit(TokenDict, nil)
		default:
			return l.errorf("unexpected character %q", r)
		}
	case ':':
		l.emit(TokenColon, nil)
	case ',':
		l.emit(TokenComma, nil)
	case '?':
		l.emit(TokenOption, nil)
	case '-':
		next, _, err := l.readRune()
		if err != nil || next != '>' {
			return l.error(err)
		}
		l.emit(TokenArrow, nil)
	// Newlines
	case '\n', lineSep, parSep:
		l.emit(TokenNewline, nil)
	case '\r':
		lf, _, err := l.readRune()
		if err != nil {
			if err == io.EOF {
				l.emit(TokenNewline, nil)
			}
			return l.error(err)
		}
		if lf != '\n' {
			l.unreadRune()
		}
		l.emit(TokenNewline, nil)
	// Comments
	case '#':
		comment, err := l.acceptUntil(func(r rune) bool {
			return r != '\n'
		})
		if err != nil {
			return l.error(err)
		}
		err = l.acceptNewline()
		if err != nil && err != io.EOF {
			return l.error(err)
		}
		l.emit(TokenComment, strings.TrimSpace(comment))

	default:
		if unicode.IsSpace(r) {
		loop:
			for {
				r, _, err := l.readRune()
				if err != nil {
					if err == io.EOF {
						l.emit(TokenWhitespace, nil)
					}
					return l.error(err)
				}
				switch r {
				case '\n', '\r', lineSep, parSep:
					l.unreadRune()
					break loop
				}
				if !unicode.IsSpace(r) {
					l.unreadRune()
					break loop
				}
			}
			l.emit(TokenWhitespace, nil)
			return l.lex
		}

		if isIdentifierChar(r, 0) {
			l.unreadRune()
			return l.lexIdentifier
		}
		return l.errorf("unexpected character %q", r)
	}
	return l.lex
}

func isIdentifierChar(r rune, i int) bool {
	return (r <= 'z' && r >= 'a') || (r <= 'Z' && r >= 'A') || (r <= '0' && r >= '9') || r == '-' || r == '_' || r == '.'
}

const (
	rname    = `([A-Z][A-Za-z0-9]*)`
	rintf    = `([A-Za-z](?:[-]*[A-Za-z0-9])*(?:\.[A-Za-z0-9](?:[-]*[A-Za-z0-9])*)+)`
	rfield   = `([A-Za-z](?:_?[A-Za-z0-9])*)`
	rkeyword = `(interface|method|error|type|any|object|string|int|float|bool)`
)

var reIdentifier = mustCompileRegexp("identifier", `(?m:^(?:`+rname+`|`+rintf+`|`+rfield+`|`+rkeyword+`))`)

var keywordTokenMap = map[string]TokenType{
	"interface": TokenInterfaceDef,
	"method":    TokenMethodDef,
	"error":     TokenErrorDef,
	"type":      TokenTypeDef,
	"any":       TokenTypeAny,
	"object":    TokenTypeObject,
	"string":    TokenTypeString,
	"int":       TokenTypeInt,
	"float":     TokenTypeFloat,
	"bool":      TokenTypeBool,
}

func (l *Lexer) lexIdentifier() stateFunc {
	ident, err := reIdentifier.Accept(l)
	if err != nil {
		return l.error(err)
	}

	// reject groups that do not take the length of the full match
	for i := 1; i < len(ident); i++ {
		if len(ident[i]) != len(ident[0]) {
			ident[i] = ""
		}
	}

	const (
		// regexp group indices
		name = 1 + iota
		intf
		field
		keyword
	)

	switch {
	// Coercion rules -- some keywords can be names, depending on when
	// they appear in the parse tree.
	case l.CoerceIdentifierType == TokenName && ident[name] != "":
		l.emit(TokenName, ident[name])
	case l.CoerceIdentifierType == TokenInterfaceName && ident[intf] != "":
		l.emit(TokenInterfaceName, ident[intf])
	case l.CoerceIdentifierType == TokenFieldName && ident[field] != "":
		l.emit(TokenFieldName, ident[field])

	// Normal rules
	case ident[keyword] != "":
		l.emit(keywordTokenMap[ident[keyword]], ident[keyword])
	case ident[name] != "":
		l.emit(TokenName, ident[name])
	case ident[intf] != "":
		l.emit(TokenInterfaceName, ident[intf])
	case ident[field] != "":
		l.emit(TokenFieldName, ident[field])
	default:
		panic("no groups matched but syntax.regexp.Accept did not return an error")
	}

	return l.lex
}
