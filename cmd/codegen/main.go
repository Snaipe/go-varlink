// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"unicode"

	"snai.pe/go-varlink/syntax"
)

//go:embed templates/*.tmpl
var templates embed.FS

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], fmt.Sprintf(format, args...))
	os.Exit(1)
}

type Context struct {
	PkgName    string
	GenErrors  bool
	GenTypes   bool
	GenClient  bool
	GenService bool
	GenMeta    bool
	Source     string
	Interface  syntax.InterfaceDef
}

func PascalCase(s string) string {
	return camelCase(s, 0, true)
}

func CamelCase(s string) string {
	return camelCase(s, 0, false)
}

func camelCase(s string, sep rune, capitalizeFirst bool) string {
	return FormatCase(s, func(r rune, i int, boundary, upper, wupper bool) (rune, rune) {
		fn := unicode.ToLower
		if (upper && wupper) || boundary && (capitalizeFirst || i != 0) {
			fn = unicode.ToUpper
		}
		var sc rune
		if boundary && i != 0 {
			sc = sep
		}
		return fn(r), sc
	})
}

func FormatCase(s string, runefunc func(r rune, i int, boundary, upper, wupper bool) (rune, rune)) string {

	in := strings.NewReader(s)

	var out strings.Builder

	var (
		prev   rune
		upper  bool // are we reading a FULL CAPS word?
		wupper bool // did we write an uppercase rune at word boundary?
		i      int
	)
	for {
		r, w, err := in.ReadRune()
		if err != nil {
			break
		}
		next, _, err := in.ReadRune()
		if err == nil {
			in.UnreadRune()
		} else {
			next = r
		}

		var boundary bool
		switch {
		case i == 0:
			boundary = true
		case unicode.IsDigit(r) != unicode.IsDigit(prev):
			boundary = true
		case unicode.IsLower(prev) && unicode.IsUpper(r):
			boundary = true
		case unicode.IsUpper(r) && unicode.IsLower(next):
			boundary = true
		case prev == '_':
			boundary = true
		}
		upper = unicode.IsUpper(r) && unicode.IsUpper(prev)

		tr, sep := runefunc(r, i, boundary, upper, wupper)
		if sep != 0 {
			out.WriteRune(sep)
		}
		if tr != '_' {
			out.WriteRune(tr)
		}
		i += w
		prev = r

		if boundary {
			wupper = unicode.IsUpper(tr)
		}
	}
	return out.String()
}

func Cast[T syntax.Type](t syntax.Type) *T {
	val, ok := t.(T)
	if !ok {
		return nil
	}
	return &val
}

var kwmap = map[string]bool{
	"break":       true,
	"case":        true,
	"chan":        true,
	"const":       true,
	"continue":    true,
	"default":     true,
	"defer":       true,
	"else":        true,
	"fallthrough": true,
	"for":         true,
	"func":        true,
	"go":          true,
	"goto":        true,
	"if":          true,
	"import":      true,
	"interface":   true,
	"map":         true,
	"package":     true,
	"range":       true,
	"return":      true,
	"select":      true,
	"struct":      true,
	"switch":      true,
	"type":        true,
	"var":         true,
}

func main() {
	var (
		context Context
		output  string
		gen     string
	)

	genmap := map[string]*bool{
		"errors":  &context.GenErrors,
		"types":   &context.GenTypes,
		"client":  &context.GenClient,
		"service": &context.GenService,
		"meta":    &context.GenMeta,
	}

	flag.StringVar(&context.PkgName, "pkgname", "", "override package name in generated code")
	flag.StringVar(&output, "output", "", "override output filename")
	flag.StringVar(&gen, "gen", "errors,types,client,service,meta", "what to generate")
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(3)
	}

	for _, name := range strings.Split(gen, ",") {
		b, ok := genmap[name]
		if !ok {
			fatalf("unknown gen type %q", name)
		}
		*b = true
	}

	if output == "" {
		output = flag.Arg(0) + ".go"
	}

	out := generate(&context, flag.Arg(0))

	if err := writeResult(output, out); err != nil {
		fatalf("%v", err)
	}
}

func writeResult(filepath string, data []byte) error {
	out, err := os.Create(filepath + ".tmp")
	if err != nil {
		return err
	}
	defer out.Close()
	defer os.Remove(out.Name())

	if _, err := out.Write(data); err != nil {
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	return os.Rename(out.Name(), filepath)
}

func generate(context *Context, filename string) []byte {
	f, err := os.Open(filename)
	if err != nil {
		fatalf("%v", err)
	}

	var s strings.Builder
	if _, err := io.Copy(&s, f); err != nil {
		fatalf("%v", err)
	}

	p := syntax.NewParser(strings.NewReader(s.String()))

	context.Source = s.String()
	context.Interface, err = p.Parse()
	if err != nil {
		fatalf("%v", err)
	}

	tmpl := template.New("").Option("missingkey=error")

	tmpl, err = tmpl.Funcs(template.FuncMap{
		"pascalCase": PascalCase,
		"camelCase":  CamelCase,
		"split":      strings.Split,
		"last":       func(s []string) string { return s[len(s)-1] },
		"errorf":     func(msg string, args ...any) (struct{}, error) { return struct{}{}, fmt.Errorf(msg, args...) },
		"list":       func(args ...any) []any { return args },
		"concat":     func(s ...string) string { return strings.Join(s, "") },
		"join": func(sep string, s ...string) string {
			s = slices.DeleteFunc(s, func(s string) bool { return s == "" })
			return strings.Join(s, sep)
		},
		"gostring": func(v any) string { return fmt.Sprintf("%#v\n", v) },
		"trim":     func(s string) string { return strings.TrimSpace(s) },
		"struct":   Cast[syntax.StructType],
		"enum":     Cast[syntax.EnumType],
		"array":    Cast[syntax.ArrayType],
		"dict":     Cast[syntax.DictType],
		"nullable": Cast[syntax.NullableType],
		"builtin":  Cast[syntax.BuiltinType],
		"named":    Cast[syntax.NamedType],
		"include": func(name string, args ...any) (string, error) {
			var in any = args
			if len(args) == 1 {
				in = args[0]
			}
			var out strings.Builder
			if err := tmpl.ExecuteTemplate(&out, name, in); err != nil {
				return "", err
			}
			return out.String(), nil
		},
		"default": func(def any, val any) any {
			if reflect.ValueOf(val).IsZero() {
				return def
			}
			return val
		},
		"escapekw": func(s string) string {
			if kwmap[s] {
				return s + "_"
			}
			return s
		},
	}).
		ParseFS(templates, "templates/*.tmpl")
	if err != nil {
		fatalf("%v", err)
	}

	var buf bytes.Buffer

	if err := tmpl.ExecuteTemplate(&buf, "package.tmpl", &context); err != nil {
		fatalf("%v", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		os.Stdout.Write(buf.Bytes())
		fatalf("%v", err)
	}

	return formatted
}
