// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"fmt"
	"path"
	"runtime/debug"
	"slices"
	"strings"

	"snai.pe/go-varlink/internal/service"
	"snai.pe/go-varlink/syntax"
)

type HandlerFunc func(w ReplyWriter, call *Call)

func (fn HandlerFunc) ServeMethod(w ReplyWriter, call *Call) {
	fn(w, call)
}

type ServeMux struct {
	patterns     []string
	handlers     map[string]MethodHandler
	descriptions map[string]string
	info         service.GetInfoOutput
}

func (mux *ServeMux) HandleFunc(pattern string, handler HandlerFunc) {
	mux.Handle(pattern, handler)
}

func (mux *ServeMux) Handle(pattern string, handler MethodHandler) {
	if _, err := path.Match(pattern, ""); err != nil {
		panic(err)
	}

	mux.patterns = append(mux.patterns, pattern)
	slices.Sort(mux.patterns)
	if mux.handlers == nil {
		mux.handlers = make(map[string]MethodHandler)
	}
	mux.handlers[pattern] = handler
}

// SetDescription sets the varlink service description for the specified
// interface name.
//
// The service description must be a valid Varlink IDL definition, and
// SetDescription panics if the description is invalid.
func (mux *ServeMux) SetDescription(intf string, desc string) {
	_, err := syntax.NewParser(strings.NewReader(desc)).Parse()
	if err != nil {
		panic(fmt.Sprintf("description for %q isn't written in the Varlink IDL: %v", err))
	}

	if mux.descriptions == nil {
		mux.descriptions = make(map[string]string)
	}
	mux.descriptions[intf] = desc
}

// SetInfo overrides the service information returned by introspection endpoints.
//
// Leaving a parameter empty means that it is reset to its default value, which
// is derived from the program's build information if available.
func (mux *ServeMux) SetInfo(vendor, product, version, url string) {
	mux.info = service.GetInfoOutput{
		Vendor:  vendor,
		Product: product,
		Version: version,
		Url:     url,
	}
}

func (mux *ServeMux) ServeMethod(w ReplyWriter, call *Call) {
	switch call.Method {
	case `org.varlink.service.GetInfo`:
		info := mux.info

		info.Interfaces = append(make([]string, 0, len(mux.descriptions)+1), "org.varlink.service")
		for intf := range mux.descriptions {
			info.Interfaces = append(info.Interfaces, intf)
		}
		slices.Sort(info.Interfaces)
		info.Interfaces = slices.Compact(info.Interfaces)

		binfo, ok := debug.ReadBuildInfo()
		if ok {
			if info.Vendor == "" {
				info.Vendor, _, _ = strings.Cut(binfo.Main.Path, "/")
			}
			if info.Product == "" {
				path := strings.Split(binfo.Path, "/")
				info.Product = path[len(path)-1] + " @ " + binfo.Main.Path
			}
			if info.Version == "" {
				info.Version = fmt.Sprintf("%v (%v)", binfo.Main.Version, binfo.GoVersion)
			}
			if info.Url == "" {
				info.Url, _, _ = strings.Cut(binfo.Main.Path, "/")
				info.Url = "https://" + info.Url
			}
		}
		w.WriteReply(info)
		return

	case `org.varlink.service.GetInterfaceDescription`:
		var (
			in  service.GetInterfaceDescriptionInput
			out service.GetInterfaceDescriptionOutput
		)
		call.Unmarshal(&in)

		if in.Interface == service.InterfaceName {
			out.Description = service.Description
		} else {
			desc, ok := mux.descriptions[in.Interface]
			if !ok {
				w.WriteError(service.InterfaceNotFound(in.Interface))
				return
			}
			out.Description = desc
		}

		w.WriteReply(&out)
		return
	}
	for _, pattern := range mux.patterns {
		if matched, _ := path.Match(pattern, call.Method); matched {
			mux.handlers[pattern].ServeMethod(w, call)
			return
		}
	}
	w.WriteError(service.MethodNotFound(call.Method))
}
