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
	Transport RoundTripper
}

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

func DoCall(method string, params any, opts ...CallOption) (*ReplyStream, error) {
	return DoCallContext(context.Background(), method, params, opts...)
}

func DoCallContext(ctx context.Context, method string, params any, opts ...CallOption) (*ReplyStream, error) {
	return DefaultClient.Call(ctx, method, params, opts...)
}
