// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"path"
	"slices"

	"snai.pe/go-varlink/internal/service"
)

type HandlerFunc func(w ReplyWriter, call *Call)

func (fn HandlerFunc) ServeMethod(w ReplyWriter, call *Call) {
	fn(w, call)
}

type ServeMux struct {
	patterns []string
	handlers map[string]MethodHandler
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

func (mux *ServeMux) ServeMethod(w ReplyWriter, call *Call) {
	for _, pattern := range mux.patterns {
		if matched, _ := path.Match(pattern, call.Method); matched {
			mux.handlers[pattern].ServeMethod(w, call)
			return
		}
	}
	w.WriteError(service.MethodNotFound(call.Method))
}
