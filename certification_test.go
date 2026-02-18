// Copyright 2026 Franklin "Snaipe" Mathieu.
//
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package varlink_test

import (
	"context"
	"math/rand/v2"
	"net"
	"reflect"
	"testing"

	"snai.pe/go-varlink"
	"snai.pe/go-varlink/org.varlink.service"
	"snai.pe/go-varlink/syntax/testdata/standard/org.varlink.certification"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString() string {
	b := make([]rune, 32)
	for i := range b {
		b[i] = letters[rand.N(len(letters))]
	}
	return string(b)
}

type Service struct {
	T        *testing.T
	ClientID string
	expected []any
}

func (s *Service) expect(kvs ...any) varlink.Error {
	for i := 0; i < len(kvs); i += 2 {
		name, value := kvs[i].(string), kvs[i+1]
		if i >= 2*len(s.expected) {
			s.T.Logf("server: parameter %s not present in expected set", name)
			return service.InvalidParameter(name)
		}
		if !reflect.DeepEqual(value, s.expected[i/2]) {
			s.T.Logf("server: parameter %s: expected %#v, got %#v", name, s.expected[i/2], value)
			return service.InvalidParameter(name)
		}
	}
	if 2*len(s.expected) > len(kvs) {
		return service.InvalidParameter("(EXTRA)")
	}
	return nil
}

func (s *Service) Start(ctx context.Context) (outClientId string, err varlink.Error) {
	return randString(), nil
}

func (s *Service) Test01(ctx context.Context, inClientId string) (outBool bool, err varlink.Error) {
	return true, s.expect("client_id", inClientId)
}

func (s *Service) Test02(ctx context.Context, inClientId string, inBool bool) (outInt int, err varlink.Error) {
	return rand.Int(), s.expect("client_id", inClientId, "bool", inBool)
}

func (s *Service) Test03(ctx context.Context, inClientId string, inInt int) (outFloat float64, err varlink.Error) {
	return rand.Float64(), s.expect("client_id", inClientId, "int", inInt)
}

func (s *Service) Test04(ctx context.Context, inClientId string, inFloat float64) (outString string, err varlink.Error) {
	return randString(), s.expect("client_id", inClientId, "float", inFloat)
}

func (s *Service) Test05(ctx context.Context, inClientId string, inString string) (outBool bool, outInt int, outFloat float64, outString string, err varlink.Error) {
	return true, rand.Int(), rand.Float64(), randString(), s.expect("client_id", inClientId, "string", inString)
}

func (s *Service) Test06(ctx context.Context, inClientId string, inBool bool, inInt int, inFloat float64, inString string) (outStruct struct {
	Bool   bool    `json:"bool"`
	Int    int     `json:"int"`
	Float  float64 `json:"float"`
	String string  `json:"string"`
}, err varlink.Error) {
	outStruct.Bool = true
	outStruct.Int = rand.Int()
	outStruct.Float = rand.Float64()
	outStruct.String = randString()
	return outStruct, s.expect("client_id", inClientId, "bool", inBool, "int", inInt, "float", inFloat, "string", inString)
}

func (s *Service) Test07(ctx context.Context, inClientId string, inStruct struct {
	Bool   bool    `json:"bool"`
	Int    int     `json:"int"`
	Float  float64 `json:"float"`
	String string  `json:"string"`
}) (outMap map[string]string, err varlink.Error) {
	return map[string]string{randString(): randString()}, s.expect("client_id", inClientId, "struct", inStruct)
}

func (s *Service) Test08(ctx context.Context, inClientId string, inMap map[string]string) (outSet map[string]struct{}, err varlink.Error) {
	return map[string]struct{}{randString(): struct{}{}}, s.expect("client_id", inClientId, "map", inMap)
}

func (s *Service) Test09(ctx context.Context, inClientId string, inSet map[string]struct{}) (outMytype certification.MyType, err varlink.Error) {
	return outMytype, s.expect("client_id", inClientId, "set", inSet)
}

// returns more than one reply with "continues"
func (s *Service) Test10(ctx context.Context, inClientId string, inMytype certification.MyType) (outString string, err varlink.Error) {
	return outString, s.expect("client_id", inClientId, "mytype", inMytype)
}

// must be called as "oneway"
func (s *Service) Test11(ctx context.Context, inClientId string, inLastMoreReplies []string) (err varlink.Error) {
	return s.expect("client_id", inClientId, "last_more_replies", inLastMoreReplies)
}

func (s *Service) End(ctx context.Context, inClientId string) (outAllOk bool, err varlink.Error) {
	return true, s.expect("client_id", inClientId)
}

type logHandler struct {
	Next varlink.MethodHandler
	T    *testing.T
}

func (l logHandler) ServeMethod(w varlink.ReplyWriter, call *varlink.Call) {
	l.T.Logf("server: %s", call.Method)
	l.Next.ServeMethod(w, call)
}

func TestCertification(t *testing.T) {

	var (
		svc    Service
		client certification.Client
		server varlink.Server
	)

	svc.T = t
	server.Handler = logHandler{Next: certification.NewHandler(&svc), T: t}

	ctx := t.Context()
	c1, c2 := net.Pipe()

	go server.ServeConn(ctx, c1)

	client.Transport = &varlink.Transport{
		Dial: func(ctx context.Context, uri string) (*varlink.Session, error) {
			return varlink.NewSession(c2), nil
		},
	}

	id, err := client.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("client: Start: generated ID %v", id)
	svc.expected = append(svc.expected[:0], id)

	_bool, err := client.Test01(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, _bool)
	t.Log("client: Test01:", _bool)

	_int, err := client.Test02(ctx, id, _bool)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, _int)
	t.Log("client: Test02:", _int)

	_float, err := client.Test03(ctx, id, _int)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, _float)
	t.Log("client: Test03:", _float)

	_string, err := client.Test04(ctx, id, _float)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, _string)
	t.Log("client: Test04:", _string)

	_bool, _int, _float, _string, err = client.Test05(ctx, id, _string)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, _bool, _int, _float, _string)
	t.Log("client: Test05:", _bool, _int, _float, _string)

	object, err := client.Test06(ctx, id, _bool, _int, _float, _string)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, object)
	t.Log("client: Test06:", object)

	_map, err := client.Test07(ctx, id, object)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, _map)
	t.Log("client: Test07:", _map)

	set, err := client.Test08(ctx, id, _map)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, set)
	t.Log("client: Test08:", set)

	named, err := client.Test09(ctx, id, set)
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, named)
	t.Log("client: Test09:", named)

	rs, err := client.Call(ctx, `org.varlink.certification.Test10`, certification.Test10Input{ClientId: id, Mytype: named}, varlink.More())
	if err != nil {
		t.Fatal(err)
	}

	var lastMoreReplies []string
	for rs.Next() {
		var out certification.Test10Output

		if err := rs.Unmarshal(&out); err != nil {
			t.Fatal(err)
		}
		t.Log("client: Test10: more reply:", out.String)

		lastMoreReplies = append(lastMoreReplies, out.String)
	}
	if err := rs.Error(); err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id, lastMoreReplies)

	_, err = client.Call(ctx, `org.varlink.certification.Test10`, certification.Test11Input{ClientId: id, LastMoreReplies: lastMoreReplies}, varlink.OneWay())
	if err != nil {
		t.Fatal(err)
	}
	svc.expected = append(svc.expected[:0], id)
	t.Log("client: Test11: oneway")

	allok, err := client.End(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("client: End:", allok)

	if !allok {
		t.Fatal("varlink server rejected certification")
	}
}
