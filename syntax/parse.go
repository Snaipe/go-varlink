// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package syntax

import (
	"io"
)

// NodeBase represents a node in a parse tree.
type Node struct {
	// The starting position of the node in the file (ignoring comments and whitespace)
	Position Cursor

	// Any comments attached to this node
	Comments []Token
}

type InterfaceDef struct {
	Node

	Name string

	Types []TypeDef

	Methods []MethodDef

	Errors []ErrorDef
}

type TypeDef struct {
	Node

	Name string

	Type Type
}

type Type interface {
	isType()
}

type Struct struct {
	Node

	Fields []StructField
}

func (Struct) isType() {}

type StructField struct {
	Node

	Name string

	Type Type
}

type Enum struct {
	Node

	Values []EnumValue
}

type EnumValue struct {
	Node

	Name string
}

func (Enum) isType() {}

type BuiltinType struct {
	Node

	Name string
}

func (BuiltinType) isType() {}

type NamedType struct {
	Node

	Name string
}

func (NamedType) isType() {}

type ArrayType struct {
	Node

	ElemType Type
}

func (ArrayType) isType() {}

type DictType struct {
	Node

	ElemType Type
}

func (DictType) isType() {}

type NullableType struct {
	Node

	Type Type
}

func (NullableType) isType() {}

type MethodDef struct {
	Node

	Name string

	Input Struct

	Output Struct
}

type ErrorDef struct {
	Node

	Name string

	Params Struct
}

type Parser struct {
	lexer *Lexer
	prev  []Token
}

func NewParser(in io.Reader) *Parser {
	p := Parser{
		lexer: NewLexer(in),
	}
	return &p
}

func (p *Parser) Next() (token Token) {
	for {
		if len(p.prev) > 0 {
			last := len(p.prev) - 1
			token, p.prev = p.prev[last], p.prev[:last]
		} else {
			token = p.lexer.Next()
		}
		switch token.Type {
		case TokenWhitespace:
			continue
		case TokenError:
			p.error(token, nil)
		}
		return token
	}
}

func (p *Parser) Back(tokens ...Token) {
	for i := range len(tokens) {
		p.prev = append(p.prev, tokens[len(tokens)-i-1])
	}
}

func (p *Parser) Peek() (token Token) {
	token = p.Next()
	p.Back(token)
	return
}

func (p *Parser) Accept(expect ...TokenType) (token Token) {
	token = p.Next()
	for _, typ := range expect {
		if token.Type == typ {
			return
		}
	}
	p.error(token, UnexpectedTokenError(expect))
	panic("unreachable")
}

func (p *Parser) error(token Token, err error) {
	if token.Type == TokenError {
		panic(token.Value.(*Error))
	}
	err = TokenTypeError{Token: token, Err: err}
	panic(&Error{Cursor: token.Start, Err: err})
}

func (p *Parser) Parse() (intf InterfaceDef, err error) {
	defer func() {
		if e := recover(); e != nil {
			if ee, ok := e.(*Error); ok {
				err = ee
			} else {
				panic(e)
			}
		}
	}()

	comments := p.Comments()

	// "interface"
	token := p.Accept(TokenInterfaceDef)
	intf.Position = token.Start
	intf.Comments = comments

	// <interface-name>
	name := p.Accept(TokenInterfaceName)
	intf.Name = name.Value.(string)

	p.Accept(TokenNewline, TokenComment)

	for {
		comments := p.Comments()
		token := p.Peek()

		switch token.Type {
		case TokenTypeDef:
			typedef := p.TypeDef()
			typedef.Comments = comments
			intf.Types = append(intf.Types, typedef)

		case TokenMethodDef:
			method := p.MethodDef()
			method.Comments = comments
			intf.Methods = append(intf.Methods, method)

		case TokenErrorDef:
			errdef := p.ErrorDef()
			errdef.Comments = comments
			intf.Errors = append(intf.Errors, errdef)

		case TokenEOF:
			return intf, nil

		default:
			p.error(token, UnexpectedTokenError{TokenTypeDef, TokenMethodDef, TokenErrorDef})
		}
	}
}

func (p *Parser) Comments() (comments []Token) {
	for {
		switch token := p.Next(); token.Type {
		case TokenComment:
			comments = append(comments, token)
		case TokenNewline:
			comments = comments[:0]
		case TokenEOF:
			return comments
		default:
			p.Back(token)
			return comments
		}
	}
}

func (p *Parser) TypeDef() (typedef TypeDef) {
	token := p.Accept(TokenTypeDef)
	typedef.Position = token.Start

	name := p.Accept(TokenName)
	typedef.Name = name.Value.(string)

	typedef.Type = p.Type()
	return
}

func (p *Parser) Type() Type {
	switch token := p.Next(); token.Type {
	case TokenOption:
		typ := NullableType{Type: p.Type()}
		typ.Position = token.Start
		return typ
	default:
		p.Back(token)
		return p.NonNullableType()
	}
}

func (p *Parser) NonNullableType() Type {
	switch token := p.Next(); token.Type {
	case TokenArray:
		typ := ArrayType{ElemType: p.Type()}
		typ.Position = token.Start
		return typ
	case TokenDict:
		typ := DictType{ElemType: p.Type()}
		typ.Position = token.Start
		return typ
	default:
		p.Back(token)
		return p.ElementType()
	}
}

func (p *Parser) ElementType() Type {
	switch token := p.Next(); token.Type {

	case TokenTypeBool, TokenTypeInt, TokenTypeString:
		typ := BuiltinType{Name: token.Type.String()}
		typ.Position = token.Start
		return typ

	case TokenTypeFloat:
		typ := BuiltinType{Name: "float64"}
		typ.Position = token.Start
		return typ

	case TokenTypeObject, TokenTypeAny:
		typ := BuiltinType{Name: "json.RawMessage"}
		typ.Position = token.Start
		return typ

	case TokenLParen:
		p.lexer.CoerceIdentifierType = TokenFieldName
		comments := p.Comments()

		firstnameOrRparen := p.Accept(TokenFieldName, TokenRParen)
		p.lexer.CoerceIdentifierType = TokenEOF

		if firstnameOrRparen.Type == TokenRParen {
			p.Back(firstnameOrRparen)
			p.Back(comments...)
			p.Back(token)
			return p.Struct()
		}

		commaOrColon := p.Accept(TokenColon, TokenComma)
		p.Back(firstnameOrRparen, commaOrColon)
		p.Back(comments...)
		p.Back(token)

		switch commaOrColon.Type {
		case TokenColon:
			return p.Struct()
		case TokenComma:
			return p.Enum()
		default:
			panic("unreachable")
		}

	case TokenName:
		typ := NamedType{Name: token.Value.(string)}
		typ.Position = token.Start
		return typ

	default:
		panic("unknown token")
	}
}

func (p *Parser) Enum() (e Enum) {
	p.lexer.CoerceIdentifierType = TokenFieldName
	defer func() { p.lexer.CoerceIdentifierType = TokenEOF }()

	start := p.Accept(TokenLParen)
	e.Position = start.Start

	next := p.Accept(TokenFieldName, TokenComment, TokenNewline)
	switch next.Type {
	case TokenFieldName:
		p.Back(next)
	case TokenComment, TokenNewline:
		// ignored
	}

	var last bool
	for {
		comments := p.Comments()

		p.lexer.CoerceIdentifierType = TokenFieldName
		name := p.Accept(TokenFieldName, TokenRParen)
		p.lexer.CoerceIdentifierType = TokenEOF

		if name.Type == TokenRParen {
			return e
		}
		if last {
			p.error(name, UnexpectedTokenError{TokenRParen})
		}

		e.Values = append(e.Values, EnumValue{})
		val := &e.Values[len(e.Values)-1]

		val.Comments = comments
		val.Name = name.Value.(string)
		val.Position = name.Start

		comma := p.Next()
		if comma.Type != TokenComma {
			// Last value may skip the comma, but requires no more
			// values after that
			last = true
			p.Back(comma)
		}

		p.lexer.CoerceIdentifierType = TokenFieldName
		next := p.Accept(TokenRParen, TokenComment, TokenNewline, TokenFieldName)
		p.lexer.CoerceIdentifierType = TokenEOF
		switch next.Type {
		case TokenFieldName:
			p.Back(next)
		case TokenComment:
			val.Comments = append(val.Comments, next)
		case TokenNewline:
			// ignored
		case TokenRParen:
			return e
		}
	}
}

func (p *Parser) Struct() (s Struct) {
	start := p.Accept(TokenLParen)
	s.Position = start.Start

	p.lexer.CoerceIdentifierType = TokenFieldName
	next := p.Accept(TokenFieldName, TokenComment, TokenNewline, TokenRParen)
	p.lexer.CoerceIdentifierType = TokenEOF
	switch next.Type {
	case TokenRParen:
		return
	case TokenFieldName:
		p.Back(next)
	case TokenComment, TokenNewline:
		// ignored
	}

	var last bool
	for {
		comments := p.Comments()

		p.lexer.CoerceIdentifierType = TokenFieldName
		name := p.Accept(TokenFieldName, TokenRParen)
		p.lexer.CoerceIdentifierType = TokenEOF

		if name.Type == TokenRParen {
			return s
		}
		if last {
			p.error(name, UnexpectedTokenError{TokenRParen})
		}

		s.Fields = append(s.Fields, StructField{})
		field := &s.Fields[len(s.Fields)-1]

		field.Comments = comments
		field.Position = name.Start
		field.Name = name.Value.(string)

		p.Accept(TokenColon)
		field.Type = p.Type()

		comma := p.Next()
		if comma.Type != TokenComma {
			// Last field may skip the comma, but requires no more
			// fields after that
			last = true
			p.Back(comma)
		}

		p.lexer.CoerceIdentifierType = TokenFieldName
		next := p.Accept(TokenRParen, TokenComment, TokenNewline, TokenFieldName)
		p.lexer.CoerceIdentifierType = TokenEOF
		switch next.Type {
		case TokenFieldName:
			p.Back(next)
		case TokenComment:
			field.Comments = append(field.Comments, next)
		case TokenNewline:
			// ignored
		case TokenRParen:
			return s
		}
	}
}

func (p *Parser) MethodDef() (method MethodDef) {
	token := p.Accept(TokenMethodDef)
	method.Position = token.Start

	p.lexer.CoerceIdentifierType = TokenName
	name := p.Accept(TokenName)
	method.Name = name.Value.(string)
	p.lexer.CoerceIdentifierType = TokenEOF

	method.Input = p.Struct()

	p.Accept(TokenArrow)

	method.Output = p.Struct()
	return
}

func (p *Parser) ErrorDef() (err ErrorDef) {
	token := p.Accept(TokenErrorDef)
	err.Position = token.Start

	name := p.Accept(TokenName)
	err.Name = name.Value.(string)

	err.Params = p.Struct()
	return
}
