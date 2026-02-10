// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"snai.pe/go-varlink/internal/service"
)

//go:generate go run snai.pe/go-varlink/cmd/codegen org.varlink.service/service.varlink

// Generate subset to avoid import cycle
//go:generate go run snai.pe/go-varlink/cmd/codegen -gen=types,errors,meta -output=internal/service/service.go org.varlink.service/service.varlink

var (
	ErrUnsupportedScheme = errors.New("unsupported scheme")
)

// Call represents a Varlink call.
type Call struct {

	// The URI to make the call to.
	URI URI `json:"-"`

	// Fully qualified method name, in the format <interface>.<method>.
	Method string `json:"method"`

	// OneWay, if true, instructs the server to suppress its reply. The server
	// must adhere to the instruction, to allow clients to associate the next
	// reply to the next call issued without oneway.
	OneWay bool `json:"oneway,omitempty"`

	// More, if true, requests possible multiple replies to the same call.
	More bool `json:"more,omitempty"`

	// Upgrade requests the connection to be taken over by a custom
	// protocol/payload.
	Upgrade bool `json:"upgrade,omitempty"`

	// Input parameters.
	Parameters json.RawMessage `json:"parameters,omitempty"`

	// FileDescriptors is a list of open file descriptors sent or received with
	// the method call.
	FileDescriptors []uintptr `json:"-"`
}

func decode(data []byte, v any) Error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var (
			ute  *json.UnmarshalTypeError
			verr Error
		)
		switch {
		case errors.As(err, &verr):
			return verr

		// This sucks, but we have to deal with string-parsing errors
		// from the json decoder until encoding/json/v2 is out, and
		// we're okay with bumping the minimum version of Go required
		// for this project.
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			p := strings.TrimPrefix(err.Error(), `json: unknown field "`)
			p = strings.TrimSuffix(p, `"`)
			return service.InvalidParameter(p)

		case errors.As(err, &ute):
			return service.InvalidParameter(ute.Field)
		}

		return NewError(`snai.pe.varlink.UnmarshalError`,
			"type", fmt.Sprintf("%T", v),
			"message", err.Error())
	}
	return nil
}

func (c *Call) Unmarshal(v any) Error {
	return decode([]byte(c.Parameters), v)
}

func MakeCall(method string, params any, opts ...CallOption) (call Call, err error) {
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return Call{}, err
		}
		call.Parameters = json.RawMessage(data)
	}

	call.Method = method

	for _, opt := range opts {
		opt.SetCallOption(&call)
	}
	return call, nil
}

type Reply struct {

	// Output parameters.
	Parameters json.RawMessage `json:"parameters"`

	// Continues, if true, instructs the client to expect multiple replies.
	Continues bool `json:"continues,omitempty"`

	// Error is the fully-qualified reverse-domain error name, and if set,
	// indicates that the method call has failed.
	Error string `json:"error,omitempty"`

	// FileDescriptors is a list of file descriptors send or received with the
	// reply.
	FileDescriptors []uintptr `json:"-"`
}

func (r *Reply) Unmarshal(v any) Error {
	return decode([]byte(r.Parameters), v)
}

func MakeReply(params any, opts ...ReplyOption) (reply Reply, err error) {
	data, err := json.Marshal(params)
	if err != nil {
		return Reply{}, err
	}

	// Never omit parameters in replies, even if params is nil. Most
	// implementations of varlink expect that field to be present and will
	// fail if an empty document is sent back as reply.
	reply.Parameters = json.RawMessage(data)

	for _, opt := range opts {
		opt.SetReplyOption(&reply)
	}
	return reply, nil
}

type URI struct {
	Scheme  string
	Address string
}

// ParseURI parses the input Varlink URI.
func ParseURI(uri string) (URI, error) {

	// This isn't a real parser at the moment, because none of the URIs
	// in the wild are using anything more complex than <scheme>:<addr>.

	scheme, rest, ok := strings.Cut(uri, ":")
	if !ok {
		return URI{}, fmt.Errorf("parsing %q: not in the form <scheme>:<addr>", uri)
	}

	addr, props, _ := strings.Cut(rest, ";")

	// Everything after ";" is called "properties" and are reserved for future
	// extensions.
	_ = props

	return URI{
		Scheme:  scheme,
		Address: addr,
	}, nil
}

func (u URI) String() string {
	return fmt.Sprintf("%s:%s", u.Scheme, u.Address)
}
