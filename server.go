// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"snai.pe/go-varlink/internal/service"
)

type MethodHandler interface {
	ServeMethod(w ReplyWriter, call *Call)
}

type ReplyWriter interface {
	Context() context.Context

	WriteError(err Error) error

	WriteReply(parameters any, opts ...ReplyOption) error

	Call(method string, params any, opts ...CallOption) (*ReplyStream, error)
}

type replyWriter struct {
	session   *Session
	ctx       context.Context
	cancel    context.CancelCauseFunc
	transport RoundTripper
	mu        sync.Mutex
	replied   bool
}

func (w *replyWriter) WriteError(err Error) error {
	return w.WriteReply(err, ErrorCode(err.ErrorCode()))
}

func (w *replyWriter) WriteReply(parameters any, opts ...ReplyOption) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}

	reply, err := MakeReply(parameters, opts...)
	if err != nil {
		return err
	}
	return w.writeReply(&reply)
}

// Call performs a method call back to the client that initiated this session.
func (w *replyWriter) Call(method string, params any, opts ...CallOption) (*ReplyStream, error) {
	if err := w.ctx.Err(); err != nil {
		return nil, err
	}

	call, err := MakeCall(method, params, opts...)
	if err != nil {
		return nil, err
	}

	return w.transport.RoundTrip(w.ctx, w.session, &call)
}

func (w *replyWriter) hasReplied() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.replied
}

func (w *replyWriter) Context() context.Context {
	return w.ctx
}

func (w *replyWriter) writeReply(reply *Reply) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.replied {
		panic("method call has already been replied to.")
	}

	payload, err := json.Marshal(reply)
	if err != nil {
		return err
	}

	if !reply.Continues {
		w.replied = true
	}
	err = w.session.WriteMsg(payload, reply.FileDescriptors)
	if errors.Is(err, ErrPeerDisconnected) {
		w.cancel(ErrPeerDisconnected)
	}
	return err
}

type Server struct {
	// Handler is the MethodHandler used to serve method calls.
	Handler MethodHandler

	// Transport is the RoundTripper that should be used when driving
	// server-to-client calls.
	//
	// If nil, DefaultTransport is used.
	Transport RoundTripper

	// MaxPipelineSize is the maximum number of calls that a session can
	// queue before the server stops actively reading from the session.
	//
	// Receiving more calls than the pipeline size isn't fatal (unless
	// PipelineOverflowErrorFunc is set, see below), but will cause the server
	// to simply be less reactive to changes on the underlying connection. For
	// instance, client disconnects will only be noticed at the next I/O
	// operation rather than immediately.
	//
	// If PipelineOverflowErrorFunc is set, then it is used to send errors back
	// to the client as a replies to calls that go over the pipeline limit.
	//
	// A value of 0 or less means that a default value of 128 is used.
	MaxPipelineSize int

	// PipelineOverflowErrorFunc, if set, returns the error that is replied to
	// any extra client call going over the pipeline limit as defined by
	// MaxPipelineSize.
	PipelineOverflowErrorFunc func(call *Call) Error
}

func (s *Server) Serve(l net.Listener) error {

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())

	defer func() {
		cancel()
		wg.Wait()
	}()

	for {
		conn, err := l.Accept()
		switch {
		case errors.Is(err, net.ErrClosed):
			return nil
		case err != nil:
			return err
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.ServeConn(ctx, conn)
		}()
	}
}

func (s *Server) ServeConn(ctx context.Context, conn net.Conn) {
	session := NewSession(conn)
	defer session.Close()

	s.ServeSession(ctx, session)
}

func (s *Server) ServeSession(ctx context.Context, session *Session) {
	transport := s.Transport
	if transport == nil {
		transport = DefaultTransport
	}

	maxPipelineSize := s.MaxPipelineSize
	if maxPipelineSize <= 0 {
		maxPipelineSize = 128
	}
	pipeline := make(chan Call, maxPipelineSize)

	ctx, cancel := context.WithCancelCause(ctx)

	go func() {
		var call Call
		for call = range pipeline {
			w := &replyWriter{
				ctx:       ctx,
				cancel:    cancel,
				session:   session,
				transport: transport,
			}

			if s.Handler == nil {
				w.WriteError(service.MethodNotFound(call.Method))
				continue
			}

			s.Handler.ServeMethod(w, &call)

			if err := ctx.Err(); err != nil {
				return
			}
			if !w.hasReplied() {
				w.WriteError(service.MethodNotImplemented(call.Method))
				continue
			}
		}
	}()

	pipelineErrorFunc := s.PipelineOverflowErrorFunc

	var call Call
	for {
		err := session.ReadCall(ctx, &call)
		switch {
		case errors.Is(err, ErrPeerDisconnected):
			cancel(ErrPeerDisconnected)
			return
		case err != nil:
			return
		}

		if pipelineErrorFunc == nil {
			select {
			case <-ctx.Done():
				return
			case pipeline <- call:
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case pipeline <- call:
			default:
				w := &replyWriter{
					ctx:     ctx,
					cancel:  cancel,
					session: session,
				}
				w.WriteError(pipelineErrorFunc(&call))
			}
		}
	}
}

func Listen(uri string) (net.Listener, error) {
	u, err := ParseURI(uri)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "tcp", "unix":
		return net.Listen(u.Scheme, u.Address)
	default:
		return nil, fmt.Errorf("listen %v: %w", u, ErrUnsupportedScheme)
	}
}

func ListenAndServe(uri string, handler MethodHandler) error {
	listener, err := Listen(uri)
	if err != nil {
		return err
	}

	server := Server{Handler: handler}
	return server.Serve(listener)
}
