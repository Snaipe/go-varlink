// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

var DefaultTransport RoundTripper = &Transport{}

type RoundTripper interface {
	RoundTrip(ctx context.Context, session *Session, call *Call) (*ReplyStream, error)
}

type Transport struct {
	// Server is varlink server used for any new session opened by the
	// transport to serve any received session calls.
	Server Server

	// MaxKeepAliveSessions defines how many sessions should be kept alive
	// per URI.
	//
	// The default is 1.
	MaxKeepAliveSessions int

	// SessionServeContext, if set, is called whenever a new session is created
	// for the specified URI, and returns the context that will be associated
	// with call handling on that session.
	SessionServeContext func(URI, *Session) context.Context

	mu       sync.Mutex
	sessions map[URI]chan *Session
}

func (ts *Transport) RoundTrip(ctx context.Context, session *Session, call *Call) (*ReplyStream, error) {
	ts.init()

	uri := call.URI
	if uri == (URI{}) {
		i := strings.LastIndexByte(call.Method, '.')
		if i == -1 {
			return nil, fmt.Errorf("call %q: malformed method name", call.Method)
		}
		intf := call.Method[:i]
		uri = URI{Scheme: "unix", Address: "@" + intf}
	}

	if session == nil {
		var err error
		session, err = ts.takeSession(ctx, call.URI)
		if err != nil {
			return nil, err
		}

		if !call.Upgrade {
			defer ts.giveSession(call.URI, session)
		}
	}

	if err := session.WriteCall(ctx, call); err != nil {
		return nil, err
	}

	return NewReplyStream(ctx, call, session), nil
}

func (ts *Transport) init() {
	ts.mu.Lock()
	if ts.sessions == nil {
		ts.sessions = make(map[URI]chan *Session)
	}
	ts.mu.Unlock()
}

func (ts *Transport) takeSession(ctx context.Context, uri URI) (*Session, error) {
	ts.mu.Lock()
	ch := ts.sessions[uri]
	if ch == nil {
		maxsessions := ts.MaxKeepAliveSessions
		if maxsessions <= 0 {
			maxsessions = 1
		}
		ch = make(chan *Session, maxsessions)
		ts.sessions[uri] = ch
	}
	ts.mu.Unlock()

	select {
	case session := <-ch:
		return session, nil
	default:
	}

	session, err := Dial(ctx, uri.String())
	if err != nil {
		return nil, err
	}

	newctx := ts.SessionServeContext
	if newctx == nil {
		newctx = func(URI, *Session) context.Context {
			return context.Background()
		}
	}

	go ts.Server.ServeSession(newctx(uri, session), session)

	return session, nil
}

func (ts *Transport) giveSession(uri URI, session *Session) {
	ts.mu.Lock()
	ch := ts.sessions[uri]
	ts.mu.Unlock()

	if ch == nil {
		panic("programming error: no associated session channel exists for uri")
	}

	select {
	case ch <- session:
	default:
		session.Close()
	}
}

func (ts *Transport) CloseIdleConnections() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	for _, ch := range ts.sessions {
		select {
		case session := <-ch:
			session.Close()
		default:
		}
	}
}

// FdPasser represents the ability for a connection to send and receive file
// descriptors.
//
// Typically, this only means unix sockets and the *UnixConn type.
type FdPasser interface {

	// PassFds queues the specified file descriptors to be sent during the next
	// write.
	PassFds(fd ...uintptr)

	// CollectFds returns any file descriptors that have been received by
	// during previous reads. Calling CollectFds resets the file descriptor
	// slice back to empty.
	//
	// The returned slice is valid up until the next read operation.
	CollectFds() []uintptr
}

type ReplyStream struct {
	ctx  context.Context
	call *Call
	sess *Session
	cur  Reply
	err  error
	more bool
}

func NewReplyStream(ctx context.Context, call *Call, session *Session) *ReplyStream {
	return &ReplyStream{ctx: ctx, call: call, sess: session, more: true}
}

// Next advances the stream by one reply, and returns whether there are
// more replies to come after this.
func (r *ReplyStream) Next() bool {
	if !r.more {
		return false
	}
	r.err = r.sess.ReadReply(r.ctx, r.call, &r.cur)
	if r.err != nil {
		r.more = false
		return false
	}

	if r.cur.Error != "" {
		r.err = &varlinkError{Code: r.cur.Error, Parameters: r.cur.Parameters}
	}
	r.more = r.cur.Continues
	return true
}

// Reply returns the current error in the stream. These can be session errors,
// or error replies. Error replies are converted and returned as Go errors.
func (r *ReplyStream) Error() error {
	return r.err
}

// Reply returns the current reply in the stream.
//
// The returned pointer is valid until Next() is called.
func (r *ReplyStream) Reply() *Reply {
	return &r.cur
}

// Unmarshal unmarshals the parameters of the current reply into the specified
// pointer value.
func (r *ReplyStream) Unmarshal(params any) Error {
	return r.cur.Unmarshal(params)
}
