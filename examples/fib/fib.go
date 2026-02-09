package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math/big"
	"strconv"
	"time"

	"snai.pe/go-varlink"
	"snai.pe/go-varlink/org.varlink.service"
)

var (
	serve = flag.Bool("serve", false, "run a ping server")
	uri   = flag.String("uri", "unix:@org.example.ping", "address to connect or listen to")
)

func main() {
	flag.Parse()

	if *serve {
		server(*uri)
	} else {
		client(*uri)
	}
}

func fib(ctx context.Context, n int) (*big.Int, error) {
	if n == 0 {
		return big.NewInt(0), nil
	}
	var a, b, tmp big.Int
	a.SetInt64(0)
	b.SetInt64(1)
	for range n - 1 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tmp.Add(&a, &b)
		a.Set(&b)
		b.Set(&tmp)
	}
	return &b, nil
}

func server(uri string) {

	var mux varlink.ServeMux

	mux.HandleFunc("org.example.fib.Fibonacci", func(rw varlink.ReplyWriter, call *varlink.Call) {

		var params struct {
			N int `json:"n"`
		}
		params.N = -1

		if len(call.Parameters) > 0 {
			err := json.Unmarshal([]byte(call.Parameters), &params)
			if err != nil {
				log.Println("failed to unmarshal", call.Parameters, err)
				rw.WriteError(service.InvalidParameter("n"))
				return
			}
		}
		if params.N < 0 {
			rw.WriteError(service.InvalidParameter("n"))
			return
		}

		ctx, cancel := context.WithCancel(rw.Context())
		defer cancel()

		// Send heartbeats back to the client to make sure it hasn't gone
		// away during the computation
		go func() {
			defer cancel()

			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r, err := rw.Call("org.example.fib.Heartbeat", nil)
					if err != nil {
						log.Printf("client missed heartbeat: call failed: %v", err)
						return
					}
					for {
						ok := r.Next() && ctx.Err() == nil
						if err := r.Error(); err != nil {
							log.Printf("client missed heartbeat: reply error: %v", err)
							return
						}
						if !ok {
							break
						}
					}
				}
			}
		}()

		log.Printf("computing fibonacci(%d)", params.N)
		result, err := fib(ctx, params.N)
		if err != nil {
			rw.WriteError(varlink.NewError("org.example.fib.ClientCanceled"))
			return
		}

		var out struct {
			Result string `json:"result"`
		}
		out.Result = result.String()

		log.Printf("done computing fibonacci(%d): result is %s", params.N, out.Result)
		rw.WriteReply(out)
	})

	if err := varlink.ListenAndServe(uri, &mux); err != nil {
		log.Fatal(err)
	}
}

func client(uri string) {

	var clientmux varlink.ServeMux
	clientmux.HandleFunc("org.example.fib.Heartbeat", func(rw varlink.ReplyWriter, call *varlink.Call) {
		rw.WriteReply(nil)
	})

	var transport varlink.Transport
	transport.Server.Handler = &clientmux

	defer transport.CloseIdleConnections()

	client := varlink.Client{
		Transport: &transport,
	}

	n, err := strconv.Atoi(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	var params struct {
		N int `json:"n"`
	}
	params.N = n

	r, err := client.Call(context.Background(), "org.example.fib.Fibonacci", &params, varlink.CallURI(uri))
	if err != nil {
		log.Fatal(err)
	}

	for r.Next() {
		var result struct {
			Result string `json:"result"`
		}
		r.Unmarshal(&result)

		log.Println(result.Result)
	}
	if err := r.Error(); err != nil {
		log.Println(err)
	}
}
