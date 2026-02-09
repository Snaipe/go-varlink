// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

//go:build unix

package varlink

import (
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

const _SCM_MAX_FD = 253 // man unix(7) documents this limit on Linux

func align2up(v, d int) int {
	return ((v - 1) & ^(d - 1)) + d
}

func makeOOBForFds() any {
	return make([]byte, syscall.CmsgSpace(_SCM_MAX_FD*4))
}

var oobPool = sync.Pool{
	New: makeOOBForFds,
}

func rawSyscall6Eintr(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, errno syscall.Errno) {
	errno = syscall.EINTR
	// Also retry on ENOBUFS -- this is a transient error that also needs to
	// be retried. Unfortunately we can't bubble this up to the RawConn.Read/Write
	// APIs because the poller is edge-triggered.
	for errno == syscall.EINTR || errno == syscall.ENOBUFS {
		r1, r2, errno = syscall.RawSyscall6(trap, a1, a2, a3, a4, a5, a6)
	}
	return
}

func recvmsgEintr(fd uintptr, p, oob []byte, flags uintptr) (n, oobn int, recvflags int, err error) {
	var iov syscall.Iovec
	iov.Base = unsafe.SliceData(p)
	iov.SetLen(len(p))

	var (
		rsa syscall.RawSockaddrAny
		msg syscall.Msghdr
	)
	msg.Name = (*byte)(unsafe.Pointer(&rsa))
	msg.Namelen = uint32(syscall.SizeofSockaddrAny)
	msg.Iov = &iov
	msg.Iovlen = 1
	msg.Control = unsafe.SliceData(oob)
	msg.SetControllen(len(oob))

	n1, _, errno := rawSyscall6Eintr(syscall.SYS_RECVMSG,
		fd,
		uintptr(unsafe.Pointer(&msg)),
		uintptr(flags),
		0,
		0,
		0,
	)
	switch errno {
	case 0:
	case syscall.EAGAIN:
		return 0, 0, 0, errno
	default:
		return 0, 0, 0, &os.SyscallError{Syscall: "recvmsg", Err: errno}
	}

	return int(n1), int(msg.Controllen), int(msg.Flags), nil
}

func sendmsgEintr(fd uintptr, p, oob []byte, flags uintptr) (n int, err error) {
	var iov syscall.Iovec
	iov.Base = unsafe.SliceData(p)
	iov.SetLen(len(p))

	var msg syscall.Msghdr
	msg.Iov = &iov
	msg.Iovlen = 1
	msg.Control = unsafe.SliceData(oob)
	msg.SetControllen(len(oob))

	n1, _, errno := rawSyscall6Eintr(syscall.SYS_SENDMSG,
		fd,
		uintptr(unsafe.Pointer(&msg)),
		uintptr(flags),
		0,
		0,
		0,
	)
	switch errno {
	case 0:
	case syscall.EAGAIN, syscall.EMSGSIZE:
		return 0, errno
	default:
		return 0, &os.SyscallError{Syscall: "sendmsg", Err: errno}
	}

	return int(n1), nil
}

func recvmsg(socket syscall.RawConn, buf, oob []byte) (obuf, ooob []byte, err error) {
	const flags = syscall.MSG_CMSG_CLOEXEC | syscall.MSG_DONTWAIT | syscall.MSG_WAITALL

	var cerr error
	err = socket.Read(func(fd uintptr) bool {
		n, oobn, _, err := recvmsgEintr(fd, buf, oob, flags)
		switch err {
		case nil:
		case syscall.EAGAIN:
			return false
		default:
			cerr = err
			return true
		}

		cerr = err
		oob = oob[:oobn]
		buf = buf[:n]
		return true
	})
	if err != nil || cerr != nil {
		return nil, nil, errors.Join(err, cerr)
	}
	if len(buf) == 0 {
		return nil, nil, io.EOF
	}
	return buf, oob, nil
}

func recv(socket syscall.RawConn, buf []byte, fds []uintptr) (int, []uintptr, error) {
	oob := make([]byte, syscall.CmsgSpace(_SCM_MAX_FD*4))

	buf, oob, err := recvmsg(socket, buf, oob)
	if err != nil {
		return 0, nil, err
	}

	cmsgs, err := syscall.ParseSocketControlMessage(oob)
	numfds := 0
	for _, cmsg := range cmsgs {
		var parsed []int
		parsed, err = syscall.ParseUnixRights(&cmsg)
		if err != nil {
			err = &os.SyscallError{Syscall: "parse unix rights", Err: err}
			break
		}
		for i, fd := range parsed {
			fds[i] = uintptr(fd)
			numfds++
		}
	}
	if err != nil {
		for _, fd := range fds {
			_ = syscall.Close(int(fd))
		}
		return 0, nil, err
	}
	return len(buf), fds[:numfds], nil
}

func sendmsg(socket syscall.RawConn, buf, oob []byte) (int, error) {
	var (
		n    int
		cerr error
	)
	err := socket.Write(func(fd uintptr) bool {
		n1, err := sendmsgEintr(fd, buf, oob, syscall.MSG_DONTWAIT)
		switch err {
		case nil:
			n = n1
		case syscall.EAGAIN:
			return false
		default:
			cerr = err
		}
		return true
	})
	if err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}

	return n, nil
}

func send(socket syscall.RawConn, buf []byte, fds []uintptr) (n int, err error) {
	if len(fds) > _SCM_MAX_FD {
		panic("programming error: cannot pass more than 253 file descriptors per message")
	}
	intfds := make([]int, len(fds))
	for i, fd := range fds {
		intfds[i] = int(fd)
	}
	oob := syscall.UnixRights(intfds...)

	written := 0
	for len(buf) > 0 {
		n, err := sendmsg(socket, buf, oob)

		written += n
		buf = buf[n:]

		if err != nil {
			return written, err
		}

		// The out of band data only gets sent once, in the first chunk.
		oob = nil
	}
	return written, nil
}

func getsndbufsz(conn syscall.RawConn) (sndbufsz int, err error) {
	conn.Control(func(fd uintptr) {
		sndbufsz, err = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	})
	if err != nil {
		err = &os.SyscallError{Syscall: "getsockopt SO_SNDBUF", Err: err}
	}
	// The send buffer needs some extra space for any message overhead.
	// Dividing the actual buffer size by 2 mimicks the kernel behaviour of
	// doubling the user-specified send buffer size when setsockopt is called,
	// leaving at least half of the buffer size for the payload.
	sndbufsz >>= 1
	return
}

func dup(fd uintptr) (uintptr, error) {
	newfd, _, errno := syscall.RawSyscall(syscall.SYS_FCNTL, fd, syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return ^uintptr(0), &os.SyscallError{Syscall: "fcntl F_DUPFD_CLOEXEC", Err: errno}
	}
	return newfd, nil
}

func sysClose(fd uintptr) error {
	return syscall.Close(int(fd))
}

type errDisconnected struct{}

func (errDisconnected) Is(err error) bool {
	switch {
	case err == (errDisconnected{}):
		fallthrough
	case errors.Is(err, syscall.EPIPE):
		return true
	}
	return false
}

func (errDisconnected) Error() string {
	return "peer disconnected"
}
