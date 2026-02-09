// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package syntax_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"snai.pe/go-varlink/syntax"
	stdtest1 "snai.pe/go-varlink/syntax/testdata/standard/org.example.encoding"
)

//go:generate go run snai.pe/go-varlink/cmd/codegen -output=testdata/standard/org.example.encoding/gen.go testdata/standard/org.example.encoding.varlink

func TestVarlinkStandardSuite(t *testing.T) {
	interfaces := map[string]syntax.InterfaceDef{
		"org.example.encoding": stdtest1.Definition,
	}

	filepath.Walk("testdata/standard", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}

		name := filepath.Base(path)
		if name[0] == '.' {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(name)
		switch ext {
		case ".varlink":
		default:
			return nil
		}

		t.Run(name[:len(name)-len(ext)], func(t *testing.T) {
			t.Parallel()

			txt, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			switch ext {
			case ".varlink":
				intf, err := syntax.NewParser(bytes.NewReader(txt)).Parse()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(interfaces[intf.Name], intf) {
					t.Logf("original: %#v\n", interfaces[intf.Name])
					t.Logf("re-encoded: %#v\n", intf)
					t.Fatalf("parsed %v interface from %v is different than expected", intf.Name, path)
				}
			}
		})

		return nil
	})
}

func BenchmarkStandardSuite(b *testing.B) {
	filepath.Walk("testdata/standard", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			b.Fatal(err)
		}

		name := filepath.Base(path)
		if name[0] == '.' {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(name)

		if ext != ".varlink" {
			return nil
		}

		b.Run(name[:len(name)-len(ext)], func(b *testing.B) {
			txt, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}

			for i := 0; i < b.N; i++ {
				_, err := syntax.NewParser(bytes.NewReader(txt)).Parse()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
		return nil
	})
}

func FuzzParser(f *testing.F) {
	err := filepath.Walk("testdata", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			f.Fatal(err)
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".varlink" {
			return nil
		}

		txt, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(txt)
		return nil
	})
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, txt []byte) {
		syntax.NewParser(bytes.NewReader(txt)).Parse()
	})
}

