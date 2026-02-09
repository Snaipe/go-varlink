package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"snai.pe/go-varlink"
	"snai.pe/go-varlink/org.varlink.service"
)

var (
	serve = flag.Bool("serve", false, "run a ping server")
	uri   = flag.String("uri", "unix:@org.example.ping", "address to connect or listen to")
	root  = flag.String("root", "/", "root directory to perform operations on")
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

	root, err := os.OpenRoot(*root)
	if err != nil {
		log.Fatal(err)
	}

	mux.HandleFunc("org.example.fdpass.Open", func(rw varlink.ReplyWriter, call *varlink.Call) {

		var params struct {
			Path string `json:"path"`
		}

		if len(call.Parameters) > 0 {
			err := json.Unmarshal([]byte(call.Parameters), &params)
			if err != nil {
				log.Println("failed to unmarshal", call.Parameters, err)
				rw.WriteError(service.InvalidParameter("path"))
				return
			}
		}
		if params.Path == "" {
			rw.WriteError(service.InvalidParameter("path"))
			return
		}
		params.Path = strings.TrimPrefix(filepath.Clean(params.Path), string(os.PathSeparator))

		f, err := root.Open(params.Path)
		if err != nil {
			rw.WriteError(varlink.NewError("org.example.fdpass.Error", "message", err.Error()))
			return
		}
		defer f.Close()

		rw.WriteReply(nil, varlink.Fd(f.Fd()))
	})

	if err := varlink.ListenAndServe(uri, &mux); err != nil {
		log.Fatal(err)
	}
}

func client(uri string) {

	var params struct {
		Path string `json:"path"`
	}
	params.Path = flag.Arg(0)

	r, err := varlink.DoCall("org.example.fdpass.Open", &params, varlink.CallURI(uri))
	if err != nil {
		log.Fatal(err)
	}

	r.Next()

	var osErr struct {
		Message string `json:"message"`
	}

	if err := r.Error(); err != nil {
		if e, ok := r.Error().(varlink.Error); ok {
			log.Fatal(string(r.Reply().Parameters))
			switch e.ErrorCode() {
			case "org.example.fdpass.Error":
				r.Unmarshal(&osErr)
				log.Fatalf("%v: %v", e.ErrorCode(), osErr.Message)
			}
		}
		log.Fatal(err)
	}
	fds := r.Reply().FileDescriptors
	if len(fds) != 1 {
		log.Fatalf("expected one file descriptor, but got %d\n", fds)
	}

	f := os.NewFile(fds[0], params.Path)
	defer f.Close()

	cmd := exec.Command(flag.Arg(1), flag.Args()[2:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{f}
	cmd.Run()
}
