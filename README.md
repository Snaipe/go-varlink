# go-varlink

[![GoDoc](https://godoc.org/snai.pe/boa?status.svg)](https://godoc.org/snai.pe/boa)
[![GitHub](https://img.shields.io/github/license/Snaipe/boa?color=brightgreen)](LICENSE)
[![Dependencies](https://img.shields.io/badge/dependencies-none!-brightgreen)](go.mod)

go-varlink is pure-Go [Varlink][varlink] client and server implementation.

## Features

* Has a `net/http`-like API, with support for hand-writing services and transport middleware.
* Supports code generation from a Varlink description file.
* Supports file descriptor passing via unix sockets as a first-class construct.

## Getting started

To install go-varlink, simply run:

```
$ go get snai.pe/go-varlink
```

To make client calls with the default Client, you can use DoCall:

```go
var in struct {
    Ping string `json:"ping"`
}
rs, err := varlink.DoCall("org.example.encoding.Ping", in)
if err != nil {
    return err
}

var out struct {
    Pong string `json:"pong"`
}
for rs.Next() {
    if err := rs.Unmarshal(&out); err != nil {
        return err
    }
    fmt.Println(out.Pong)
}
if err := rs.Error(); err != nil {
    return err
}
```

To write a service, you can start a server with your own method handler:

```
import (
    "snai.pe/go-varlink/org.varlink.service" // for error types
)

...

var mux varlink.ServeMux

mux.HandleFunc("org.example.encoding.Ping", func (w ReplyWriter, call *Call) {
    var in struct {
        Ping string `json:"ping"`
    }
    if err := call.Unmarshal(&in); err != nil {
        w.WriteError(service.InvalidParameter("ping"))
        return
    }

    var out struct {
        Pong string `json:"pong"`
    }
    out.Pong = in.Ping

    w.WriteReply(&out)
})

varlink.ListenAndServer("unix:@org.example.encoding", &mux)
```

More examples are available under the ./examples directory.

## Code generation

go-varlink provides a code generator that turns files written in the Varlink
Interface Definition Language into Go code.

Write your service definition in a file named service.varlink:

```varlink
# Example Varlink service
interface org.example.encoding

type State (
  start: ?bool,
  progress: ?int,
  end: ?bool
)

type Shipment (
  name: string,
  description: string,
  size: int,
  weight: ?int
)

type Order (
  shipments: []Shipment,
  order_num: int,
  customer: string
)

# Returns the same string
method Ping(ping: string) -> (pong: string)

# Returns a fake order given an order number
method GetOrder(num: int) -> (order: Order)
```

Add this go:generate command to any of your Go files to enable code generation:

```
//go:generate go run snai.pe/go-varlink/cmd/codegen service.varlink
```

Then run `go generate .`. This will create a `service.varlink.go` file
containing the type definitions, client methods, and service handlers
corresponding to this interface.

The code generator can be configured; see `go run snai.pe/go-varlink/cmd/codegen -h`
for more information.
