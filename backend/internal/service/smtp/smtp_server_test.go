// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package smtp

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is a minimal SMTP server on 127.0.0.1:0. It accepts one session
// at a time, records what it was sent, and can be told to refuse one command.
type fakeServer struct {
	t  *testing.T
	ln net.Listener

	greeting string // the banner; "220 ..." unless a test wants it refused
	failOn   string // command verb answered with failCode
	failCode string
	hangUp   string // command verb after whose reply the connection is reset

	mu    sync.Mutex
	from  string
	rcpt  []string
	auth  string
	data  string
	done  chan struct{}
	verbs []string
}

func newFakeServer(t *testing.T, ln net.Listener) *fakeServer {
	t.Helper()
	s := &fakeServer{t: t, ln: ln, greeting: "220 fake ESMTP", done: make(chan struct{})}
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeServer) start() {
	go func() {
		defer close(s.done)
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		s.serve(conn)
	}()
}

func (s *fakeServer) wait() {
	s.t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		s.t.Fatal("the SMTP session did not end")
	}
}

func (s *fakeServer) port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *fakeServer) serve(conn net.Conn) {
	tp := textproto.NewConn(conn)
	reply := func(line string) bool { return tp.PrintfLine("%s", line) == nil }
	if !reply(s.greeting) || !strings.HasPrefix(s.greeting, "220") {
		return
	}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.Fields(line + " x")[0])
		s.mu.Lock()
		s.verbs = append(s.verbs, verb)
		s.mu.Unlock()
		if verb == s.failOn {
			reply(s.failCode + " refused")
			continue
		}
		switch verb {
		case "EHLO", "HELO":
			reply("250-fake")
			reply("250 AUTH PLAIN")
		case "AUTH":
			s.mu.Lock()
			s.auth = line
			s.mu.Unlock()
			reply("235 ok")
		case "MAIL":
			s.mu.Lock()
			s.from = line
			s.mu.Unlock()
			reply("250 ok")
		case "RCPT":
			s.mu.Lock()
			s.rcpt = append(s.rcpt, line)
			s.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 go ahead")
			if s.hangUp == "DATA" {
				// Reset rather than close, so the client's next write fails.
				if tcp, ok := underlyingTCP(conn); ok {
					_ = tcp.SetLinger(0)
				}
				return
			}
			body, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.data = string(body)
			s.mu.Unlock()
			if s.failOn == "." {
				reply(s.failCode + " message refused")
				continue
			}
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unknown")
		}
	}
}

func underlyingTCP(conn net.Conn) (*net.TCPConn, bool) {
	if tc, ok := conn.(*tls.Conn); ok {
		conn = tc.NetConn()
	}
	tcp, ok := conn.(*net.TCPConn)
	return tcp, ok
}

func listenPlain(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// listenTLS listens with the standard library's fixed test certificate and
// makes this process trust it. sendWithTLS verifies against the system roots
// and takes no pool of its own, so SSL_CERT_FILE is the only way in. The
// roots are loaded once per process; that is harmless here because the
// certificate is the same one every time.
func listenTLS(t *testing.T) net.Listener {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	ts.StartTLS()
	cert := ts.TLS.Certificates[0]
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw})
	ts.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", caFile)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("listen tls: %v", err)
	}
	return ln
}

func TestSend_PlainDeliversTheMessage(t *testing.T) {
	srv := newFakeServer(t, listenPlain(t))
	srv.start()

	svc := New(Config{Enabled: true, Host: "127.0.0.1", Port: srv.port(), Username: "u", Password: "p", From: "AgentRQ <noreply@example.com>"})
	err := svc.Send(context.Background(), SendRequest{
		To:      []string{"Ada <ada@example.com>", "not an address"},
		Subject: "Hello",
		Body:    "the body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	srv.wait()

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.from != "MAIL FROM:<noreply@example.com>" {
		t.Errorf("MAIL = %q, want the bare sender address", srv.from)
	}
	// A parseable recipient is reduced to its address; an unparseable one is
	// passed on as written.
	wantRcpt := []string{"RCPT TO:<ada@example.com>", "RCPT TO:<not an address>"}
	if strings.Join(srv.rcpt, "|") != strings.Join(wantRcpt, "|") {
		t.Errorf("RCPT = %q, want %q", srv.rcpt, wantRcpt)
	}
	for _, want := range []string{
		"From: AgentRQ <noreply@example.com>\n",
		"To: Ada <ada@example.com>, not an address\n",
		"Subject: Hello\n",
		"Content-Type: text/plain; charset=UTF-8\n",
		"\nthe body",
	} {
		if !strings.Contains(srv.data, want) {
			t.Errorf("message %q lacks %q", srv.data, want)
		}
	}
}

func TestSendWithTLS_DeliversTheMessage(t *testing.T) {
	srv := newFakeServer(t, listenTLS(t))
	srv.start()

	s := &service{cfg: Config{Enabled: true, Host: "127.0.0.1", Port: 465, Username: "u", Password: "p", From: "noreply@example.com"}}
	addr := "127.0.0.1:" + strconv.Itoa(srv.port())
	auth := smtp.PlainAuth("", "u", "p", "127.0.0.1")
	err := s.sendWithTLS(context.Background(), addr, auth, "noreply@example.com", []string{"a@example.com", "b@example.com"}, []byte("Subject: x\r\n\r\nhi"))
	if err != nil {
		t.Fatalf("sendWithTLS: %v", err)
	}
	srv.wait()

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.HasPrefix(srv.auth, "AUTH PLAIN ") {
		t.Errorf("AUTH = %q, want PLAIN credentials", srv.auth)
	}
	if srv.from != "MAIL FROM:<noreply@example.com>" {
		t.Errorf("MAIL = %q", srv.from)
	}
	if len(srv.rcpt) != 2 {
		t.Errorf("RCPT = %q, want both recipients", srv.rcpt)
	}
	if !strings.Contains(srv.data, "hi") {
		t.Errorf("message = %q, want the body", srv.data)
	}
}

func TestSendWithTLS_ReportsEachRefusal(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s *fakeServer)
		msg   []byte
	}{
		{"greeting refused", func(s *fakeServer) { s.greeting = "554 not today" }, nil},
		{"auth refused", func(s *fakeServer) { s.failOn, s.failCode = "AUTH", "535" }, nil},
		{"sender refused", func(s *fakeServer) { s.failOn, s.failCode = "MAIL", "550" }, nil},
		{"recipient refused", func(s *fakeServer) { s.failOn, s.failCode = "RCPT", "550" }, nil},
		{"data refused", func(s *fakeServer) { s.failOn, s.failCode = "DATA", "554" }, nil},
		{"message refused", func(s *fakeServer) { s.failOn, s.failCode = ".", "552" }, nil},
		// The connection is reset after 354; a message far larger than the
		// write buffer makes the write itself hit the reset.
		{"connection lost while writing", func(s *fakeServer) { s.hangUp = "DATA" }, []byte(strings.Repeat("x", 4<<20))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, listenTLS(t))
			tc.setup(srv)
			srv.start()

			s := &service{cfg: Config{Host: "127.0.0.1"}}
			addr := "127.0.0.1:" + strconv.Itoa(srv.port())
			auth := smtp.PlainAuth("", "u", "p", "127.0.0.1")
			msg := tc.msg
			if msg == nil {
				msg = []byte("hi")
			}
			if err := s.sendWithTLS(context.Background(), addr, auth, "a@example.com", []string{"b@example.com"}, msg); err == nil {
				t.Fatal("sendWithTLS returned no error")
			}
			srv.wait()
		})
	}
}
