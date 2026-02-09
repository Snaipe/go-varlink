// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

// A CallOption is any option that applies to a method call.
type CallOption interface {
	SetCallOption(*Call) error
}

type funcCallOpt func(*Call) error

func (fn funcCallOpt) SetCallOption(opts *Call) error {
	return fn(opts)
}

// OneWay instructs the server to suppress its reply.
func OneWay() CallOption {
	return funcCallOpt(func(opts *Call) error {
		opts.OneWay = true
		return nil
	})
}

// More requests possible multiple replies to the same call.
func More() CallOption {
	return funcCallOpt(func(opts *Call) error {
		opts.More = true
		return nil
	})
}

// Upgrade requests the connection to be taken over by a custom protocol/payload.
func Upgrade() CallOption {
	return funcCallOpt(func(opts *Call) error {
		opts.Upgrade = true
		return nil
	})
}

// CallURI sets the URI for the call
func CallURI(uri string) CallOption {
	return funcCallOpt(func(opts *Call) error {
		u, err := ParseURI(uri)
		if err == nil {
			opts.URI = u
		}
		return err
	})
}

// A ReplyOption is any option that applies to a method reply
type ReplyOption interface {
	SetReplyOption(*Reply) error
}

type funcReplyOpt func(*Reply) error

func (fn funcReplyOpt) SetReplyOption(opts *Reply) error {
	return fn(opts)
}

// Continues signifies that more replies are coming after this reply. Must
// only be set if the call set the `more` option.
func Continues() ReplyOption {
	return funcReplyOpt(func(opts *Reply) error {
		opts.Continues = true
		return nil
	})
}

// ErrorCode turns the reply into an error reply with the specified error code.
// The error code must be a fully qualified error name (e.g. com.example.Error).
func ErrorCode(code string) ReplyOption {
	return funcReplyOpt(func(opts *Reply) error {
		opts.Error = code
		return nil
	})
}

// A MethodOption is an option that applies both to method calls and replies.
type MethodOption interface {
	CallOption
	ReplyOption
}

type funcMethodOpt struct {
	callopt  funcCallOpt
	replyopt funcReplyOpt
}

func (fn funcMethodOpt) SetCallOption(opts *Call) error {
	return fn.callopt(opts)
}

func (fn funcMethodOpt) SetReplyOption(opts *Reply) error {
	return fn.replyopt(opts)
}

// Fd attaches a file descriptor to be sent with the call or reply.
func Fd(fd uintptr) MethodOption {
	return funcMethodOpt{
		callopt: func(opts *Call) error {
			opts.FileDescriptors = append(opts.FileDescriptors, fd)
			return nil
		},
		replyopt: func(opts *Reply) error {
			opts.FileDescriptors = append(opts.FileDescriptors, fd)
			return nil
		},
	}
}
