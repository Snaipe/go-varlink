// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"context"
)

var DefaultClient = &Client{}

type Client struct {
	// The RoundTripper to make calls with. If nil, DefaultTransport is used.
	Transport RoundTripper
}

// Call performs a method call with the specified parameters and options using
// the underlying Transport.
func (client *Client) Call(ctx context.Context, method string, params any, opts ...CallOption) (*ReplyStream, error) {
	call, err := MakeCall(method, params, opts...)
	if err != nil {
		return nil, err
	}

	transport := client.Transport
	if transport == nil {
		transport = DefaultTransport
	}

	return transport.RoundTrip(ctx, nil, &call)
}

// DoCall performs a method call with the default client and context.Background().
func DoCall(method string, params any, opts ...CallOption) (*ReplyStream, error) {
	return DoCallContext(context.Background(), method, params, opts...)
}

// DoCallContext performs a method call with the default client.
func DoCallContext(ctx context.Context, method string, params any, opts ...CallOption) (*ReplyStream, error) {
	return DefaultClient.Call(ctx, method, params, opts...)
}
