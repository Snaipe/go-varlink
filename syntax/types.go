// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package syntax

// Node represents a node in the AST. All AST types embed this.
type Node struct {
	// The starting position of the node in the file (ignoring comments and whitespace)
	Position Cursor

	// Any comments attached to this node
	Comments []Token
}

// InterfaceDef is the definition of a varlink interface.
type InterfaceDef struct {
	Node

	// The fully-qualified name of the interface.
	Name string

	// Types defined in this interface.
	Types []TypeDef

	// Methods defined in this interface.
	Methods []MethodDef

	// Error types defined in this interface.
	Errors []ErrorDef
}

// TypeDef is the definition of a named varlink type.
type TypeDef struct {
	Node

	// Name of the type.
	Name string

	// Type definition.
	Type Type
}

// Type is the interface representing all possible AST nodes representing a varlink type.
type Type interface {
	isType()
}

// StructType is a Type defining a structure.
type StructType struct {
	Node

	// The fields making up this struct.
	Fields []StructField
}

func (StructType) isType() {}

// StructField represents a struct field.
type StructField struct {
	Node

	// The name of the field.
	Name string

	// The type of the field.
	Type Type
}

// EnumType is a Type defining an enumeration.
type EnumType struct {
	Node

	// The values that make up the enumeration.
	Values []EnumValue
}

// EnumValue is an enumeration value.
type EnumValue struct {
	Node

	// The name of the enumeration value.
	Name string
}

func (EnumType) isType() {}

// BuiltinType is a builtin type -- bool, int, float, string, object, any.
type BuiltinType struct {
	Node

	Name string
}

func (BuiltinType) isType() {}

// NamedType is a user-defined named type.
type NamedType struct {
	Node

	// The name of the type.
	Name string
}

func (NamedType) isType() {}

// ArrayType is a Type representing an array.
type ArrayType struct {
	Node

	// The type of the array elements.
	ElemType Type
}

func (ArrayType) isType() {}

// DictType is a Type representing a map[string]T.
type DictType struct {
	Node

	// The type of the map values.
	ElemType Type
}

func (DictType) isType() {}

// NullableType is a Type representing a nullable/optional type.
type NullableType struct {
	Node

	// The type that is made nullable.
	Type Type
}

func (NullableType) isType() {}

// MethodDef represents a method definition.
type MethodDef struct {
	Node

	// The name of the method.
	Name string

	// The input parameters.
	Input StructType

	// The output parameters.
	Output StructType
}

// ErrorDef represents an error type definition.
type ErrorDef struct {
	Node

	// The name of the error type.
	Name string

	// The parameters of the error.
	Params StructType
}
