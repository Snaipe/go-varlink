// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"context"
	"sync"
)

// cond is like sync.Cond, but takes in a context on Wait.
// Unlike sync.Cond, it is necessary to hold L when calling Signal and Broadcast.
type cond struct {
	L    sync.Locker
	wake chan struct{}
}

func makeCond(l sync.Locker) cond {
	return cond{
		L:    l,
		wake: make(chan struct{}, 1),
	}
}

func (c *cond) Broadcast() {
	close(c.wake)
	c.wake = make(chan struct{}, 1)
}

func (c *cond) Signal() {
	select {
	case c.wake <- struct{}{}:
	default:
		// if pushing to c.wake would block, it means someone already
		// signaled, and there is nothing to do
	}
}

func (c *cond) Wait(ctx context.Context) error {
	wake := c.wake
	c.L.Unlock()
	defer c.L.Lock()
	select {
	case <-wake:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
