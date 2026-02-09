// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink

import (
	"encoding/json"
	"fmt"
)

var ErrPeerDisconnected errDisconnected

// Error represents all varlink errors. Errors consist of a fully qualified
// error code in the form of (e.g. org.interface.ErrorType), and parameters.
//
// Parameters are obtained by json-marshaling the error value. Errors may
// implement json.Marshaler to customize that behaviour.
type Error interface {
	error

	ErrorCode() string
}

type varlinkError struct {
	Code       string
	Parameters json.RawMessage
}

func NewError(code string, kvs ...any) Error {
	if len(kvs)%2 != 0 {
		panic("programming error: key-value pair list has odd number of elements")
	}

	params := make(map[string]any, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		key, val := kvs[i].(string), kvs[i+1]
		params[key] = val
	}

	verr := &varlinkError{Code: code}

	if len(params) != 0 {
		data, err := json.Marshal(params)
		if err != nil {
			panic(fmt.Sprintf("NewVarlinkError: values don't marshal: %v", err))
		}

		verr.Parameters = json.RawMessage(data)
	}

	return verr
}

func (err *varlinkError) Error() string {
	return err.Code
}

func (err *varlinkError) ErrorCode() string {
	return err.Code
}

func (err *varlinkError) MarshalJSON() ([]byte, error) {
	return []byte(err.Parameters), nil
}
