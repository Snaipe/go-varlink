package main

import (
	"encoding/json"
	"flag"
	"log"
	"time"

	"snai.pe/go-varlink"
	"snai.pe/go-varlink/org.varlink.service"
)

var (
	serve = flag.Bool("serve", false, "run a ping server")
	uri   = flag.String("uri", "unix:@org.example.ping", "address to connect or listen to")
	more  = flag.Bool("more", false, "continuously ask for pings")
)

func main() {
	flag.Parse()

	if *serve {
		server(*uri)
	} else {
		client(*uri)
	}
}

func server(uri string) {

	var mux varlink.ServeMux

	mux.HandleFunc("org.example.ping.Ping", func(rw varlink.ReplyWriter, call *varlink.Call) {

		var params struct {
			Echo string `json:"echo"`
		}

		params.Echo = "Ping!"

		if len(call.Parameters) > 0 {
			err := json.Unmarshal([]byte(call.Parameters), &params)
			if err != nil {
				log.Println("failed to unmarshal", call.Parameters, err)
				rw.WriteError(service.InvalidParameter("echo"))
				return
			}
		}

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		var opts []varlink.ReplyOption
		if call.More {
			opts = append(opts, varlink.Continues())
		}

		for {
			log.Println("sending ping")
			rw.WriteReply(params, opts...)

			if !call.More {
				return
			}
			select {
			case <-ticker.C:
			case <-rw.Context().Done():
				log.Println("client disconnected")
				return
			}
		}
	})

	if err := varlink.ListenAndServe(uri, &mux); err != nil {
		log.Fatal(err)
	}
}

func client(uri string) {

	var params struct {
		Echo string `json:"echo,omitempty"`
	}

	opts := []varlink.CallOption{
		varlink.CallURI(uri),
	}
	if *more {
		opts = append(opts, varlink.More())
	}

	rs, err := varlink.DoCall("org.example.ping.Ping", &params, opts...)
	if err != nil {
		log.Fatal(err)
	}

	for rs.Next() {
		rs.Unmarshal(&params)

		log.Println(params.Echo)
	}
	if err := rs.Error(); err != nil {
		log.Fatal(err)
	}
}
