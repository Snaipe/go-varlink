// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"io"
	"net"
	"sync"
	"time"
)

type UnixConn struct {
	conn *net.UnixConn
	rfds []uintptr
	wfds []uintptr
	rmu  sync.Mutex
	wmu  sync.Mutex
}

func (u *UnixConn) Read(b []byte) (n int, err error) {
	u.rmu.Lock()
	defer u.rmu.Unlock()

	sysconn, err := u.conn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var fdbuf [_SCM_MAX_FD]uintptr

	n, fds, err := recv(sysconn, b, fdbuf[:])
	u.rfds = append(u.rfds, fds...)
	return n, err
}

func (u *UnixConn) Write(b []byte) (n int, err error) {
	u.wmu.Lock()
	defer u.wmu.Unlock()

	if len(u.wfds) > _SCM_MAX_FD {
		panic("programming error: cannot pass more than 253 file descriptors per write")
	}
	sysconn, err := u.conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	n, err = send(sysconn, b, u.wfds)
	u.wfds = u.wfds[:0]
	if err == nil && len(b) != n {
		err = io.ErrShortWrite
	}
	return n, err
}

func (u *UnixConn) CloseRead() error {
	err := u.conn.CloseRead()

	u.rmu.Lock()
	defer u.rmu.Unlock()

	for _, fd := range u.rfds {
		_ = sysClose(fd)
	}
	u.rfds = nil
	return err
}

func (u *UnixConn) CloseWrite() error {
	err := u.conn.CloseWrite()

	u.wmu.Lock()
	u.wfds = nil
	u.wmu.Unlock()

	return err
}

func (u *UnixConn) Close() error {
	err := u.conn.Close()

	u.rmu.Lock()
	u.wmu.Lock()
	defer u.wmu.Unlock()
	defer u.rmu.Unlock()

	for _, fd := range u.rfds {
		_ = sysClose(fd)
	}
	u.rfds = nil
	u.wfds = nil
	return err
}

func (u *UnixConn) PassFds(fds ...uintptr) {
	u.wfds = append(u.wfds, fds...)
}

func (u *UnixConn) CollectFds() (fds []uintptr) {
	fds, u.rfds = u.rfds, u.rfds[:0]
	return
}

func (u *UnixConn) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

func (u *UnixConn) RemoteAddr() net.Addr {
	return u.conn.RemoteAddr()
}

func (u *UnixConn) SetDeadline(t time.Time) error {
	return u.conn.SetDeadline(t)
}

func (u *UnixConn) SetReadDeadline(t time.Time) error {
	return u.conn.SetReadDeadline(t)
}

func (u *UnixConn) SetWriteDeadline(t time.Time) error {
	return u.conn.SetWriteDeadline(t)
}

var _ FdPasser = (*UnixConn)(nil)
