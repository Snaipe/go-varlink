// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

var (
	ErrFdPassingNotSupported = errors.New("file descriptor passing is not supported on this net.Conn")
)

type Session struct {
	conn     net.Conn
	wmu      sync.Mutex
	rcond    cond
	cond     cond
	rw       bufio.ReadWriter
	cq       []Call
	rq       []Reply
	inflight []*Call
	reading  bool
}

func NewSession(conn net.Conn) *Session {
	switch c := conn.(type) {
	case *net.UnixConn:
		conn = &UnixConn{conn: c}
	}

	sess := &Session{
		conn:  conn,
		cond:  makeCond(&sync.Mutex{}),
		rcond: makeCond(&sync.Mutex{}),

		rw: bufio.ReadWriter{
			Reader: bufio.NewReader(conn),
			Writer: bufio.NewWriter(conn),
		},
	}
	return sess
}

func (session *Session) WriteCall(ctx context.Context, call *Call) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	payload, err := json.Marshal(call)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := session.WriteMsg(payload, call.FileDescriptors); err != nil {
		return err
	}

	session.cond.L.Lock()
	session.inflight = append(session.inflight, call)
	session.cond.L.Unlock()

	return nil
}

func (session *Session) readCallOrReply(ctx context.Context, reply *Reply, call *Call) (isCall bool, err error) {

	// These look like a bug, but they are not. readCallOrReply is done while
	// the rcond _is unlocked_ and must relock itself afterwards.
	session.rcond.L.Unlock()
	defer session.rcond.L.Lock()

	var msg struct {
		Method     *string         `json:"method"`
		OneWay     bool            `json:"oneway"`
		More       bool            `json:"more"`
		Upgrade    bool            `json:"upgrade"`
		Continues  bool            `json:"continues"`
		Error      string          `json:"error"`
		Parameters json.RawMessage `json:"parameters"`
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	payload, fds, err := session.readMsgUnlocked()
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal(payload, &msg); err != nil {
		return false, err
	}

	isCall = msg.Method != nil

	if !isCall {
		*reply = Reply{
			Parameters:      msg.Parameters,
			Error:           msg.Error,
			Continues:       msg.Continues,
			FileDescriptors: fds,
		}
	} else {
		*call = Call{
			Method:          *msg.Method,
			OneWay:          msg.OneWay,
			More:            msg.More,
			Upgrade:         msg.Upgrade,
			Parameters:      msg.Parameters,
			FileDescriptors: fds,
		}
	}
	return isCall, nil
}

func (session *Session) waitCall(ctx context.Context, initiator *Call) error {
	session.cond.L.Lock()
	defer session.cond.L.Unlock()

	for len(session.inflight) > 0 && session.inflight[0] != initiator {
		if err := session.cond.Wait(ctx); err != nil {
			return err
		}
	}

	if len(session.inflight) == 0 {
		panic("programming error: ReadReply called but no rpc calls have been initiated")
	}
	return nil
}

func (session *Session) ReadReply(ctx context.Context, initiator *Call, reply *Reply) error {

	if err := session.waitCall(ctx, initiator); err != nil {
		return err
	}

	if err := session.readReply(ctx, reply); err != nil {
		return err
	}

	if !reply.Continues {
		session.cond.L.Lock()
		session.inflight = session.inflight[1:]
		session.cond.Broadcast()
		session.cond.L.Unlock()
	}
	return nil
}

func (session *Session) readReply(ctx context.Context, reply *Reply) error {
	session.rcond.L.Lock()
	defer session.rcond.L.Unlock()

	for session.reading && len(session.rq) == 0 {
		if err := session.rcond.Wait(ctx); err != nil {
			return err
		}
	}

	if len(session.rq) > 0 {
		*reply, session.rq = session.rq[0], session.rq[1:]
		return nil
	}

	session.reading = true
	defer func() {
		session.reading = false
	}()

	var call Call
	for {
		isCall, err := session.readCallOrReply(ctx, reply, &call)
		session.rcond.Broadcast()

		if err != nil {
			return err
		}
		if !isCall {
			return nil
		}

		session.cq = append(session.cq, call)
	}
}

func (session *Session) ReadCall(ctx context.Context, call *Call) error {
	session.rcond.L.Lock()
	defer session.rcond.L.Unlock()

	for session.reading && len(session.cq) == 0 {
		if err := session.rcond.Wait(ctx); err != nil {
			return err
		}
	}

	if len(session.cq) > 0 {
		*call, session.cq = session.cq[0], session.cq[1:]
		return nil
	}

	session.reading = true
	defer func() {
		session.reading = false
	}()

	var reply Reply
	for {
		isCall, err := session.readCallOrReply(ctx, &reply, call)
		session.rcond.Broadcast()

		if err != nil {
			return err
		}
		if isCall {
			return nil
		}

		session.rq = append(session.rq, reply)
	}
}

func (session *Session) WriteMsg(msg []byte, fds []uintptr) error {
	session.wmu.Lock()
	defer session.wmu.Unlock()

	fdpass, ok := session.conn.(FdPasser)
	if len(fds) > 0 && !ok {
		return ErrFdPassingNotSupported
	}

	if _, err := session.rw.Write(msg); err != nil {
		return err
	}

	if len(fds) > 0 {
		fdpass.PassFds(fds...)
	}

	if _, err := session.rw.Write([]byte("\x00")); err != nil {
		return err
	}

	return session.rw.Flush()
}

func (session *Session) readMsgUnlocked() (msg []byte, fds []uintptr, err error) {
	msg, err = session.rw.ReadBytes('\x00')
	switch {
	case err == io.EOF:
		return nil, nil, ErrPeerDisconnected
	case err != nil:
		return nil, nil, err
	}

	if fdpass, ok := session.conn.(FdPasser); ok {
		fds = fdpass.CollectFds()
	}

	return msg[:len(msg)-1], fds, nil
}

func (session *Session) Hijack() (conn net.Conn, rbuf []byte, err error) {
	session.wmu.Lock()
	session.rcond.L.Lock()
	defer session.rcond.L.Unlock()
	defer session.wmu.Unlock()

	conn = session.conn
	rbuf, err = session.rw.Peek(session.rw.Reader.Buffered())
	session.conn = nil
	return conn, rbuf, err
}

func (session *Session) Close() error {
	session.wmu.Lock()
	session.rcond.L.Lock()
	defer session.rcond.L.Unlock()
	defer session.wmu.Unlock()

	return session.conn.Close()
}

func Dial(ctx context.Context, uri string) (*Session, error) {
	u, err := ParseURI(uri)
	if err != nil {
		return nil, err
	}

	var conn net.Conn
	switch u.Scheme {
	case "tcp", "unix":
		conn, err = net.Dial(u.Scheme, u.Address)
	default:
		err = fmt.Errorf("dial %v: %w", u, ErrUnsupportedScheme)
	}
	if err != nil {
		return nil, err
	}

	return NewSession(conn), nil
}
