// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

var testManifest = Manifest{Name: "t", Version: "1", Inputs: []Input{{Name: "s", Type: "string", Secret: true, Required: true}}, Events: []Event{{Name: "e"}}}

func TestRunPrintsTheManifest(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"hook", "manifest"}, nil, &out, testManifest, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var m Manifest
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.ABIVersion != ABIVersion || m.Name != "t" || len(m.Events) != 1 {
		t.Errorf("manifest = %+v", m)
	}
}

func TestRunHandlesARequest(t *testing.T) {
	stdin := strings.NewReader(`{"registrationId":"r1","request":{"headers":{"x-a":["1","2"]},"body":"aGk="},"inputs":{"s":"secret","list":["a","b"]}}`)
	var got In
	var out bytes.Buffer
	code := run([]string{"hook", "handle"}, stdin, &out, testManifest, func(in In) Result {
		got = in
		return Accept(Out{Name: "e", Payload: map[string]int{"n": 1}})
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got.RegistrationID != "r1" || string(got.Request.Body) != "hi" || got.Request.Header("X-A") != "1" ||
		got.Request.Header("x-missing") != "" || got.String("s") != "secret" || strings.Join(got.Strings("list"), ",") != "a,b" ||
		got.String("missing") != "" {
		t.Errorf("in = %+v", got)
	}
	if want := `{"accept":true,"events":[{"name":"e","payload":{"n":1}}]}` + "\n"; out.String() != want {
		t.Errorf("out = %s, want %s", out.String(), want)
	}
}

func TestRunRejects(t *testing.T) {
	var out bytes.Buffer
	run([]string{"hook", "handle"}, strings.NewReader(`{}`), &out, testManifest, func(In) Result { return Reject("bad signature") })
	if want := `{"accept":false,"reason":"bad signature"}` + "\n"; out.String() != want {
		t.Errorf("out = %s", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRunFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		out   interface{ Write([]byte) (int, error) }
		want  int
	}{
		{"no command", []string{"hook"}, "", &bytes.Buffer{}, 2},
		{"an unknown command", []string{"hook", "serve"}, "", &bytes.Buffer{}, 2},
		{"input not JSON", []string{"hook", "handle"}, "nope", &bytes.Buffer{}, 1},
		{"output cannot be written", []string{"hook", "manifest"}, "", failingWriter{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := run(tc.args, strings.NewReader(tc.stdin), tc.out, testManifest, func(In) Result { return Accept() }); code != tc.want {
				t.Errorf("exit %d, want %d", code, tc.want)
			}
		})
	}
}

func TestValidHMACSHA256(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("k"))
	mac.Write([]byte("body"))
	sig := hex.EncodeToString(mac.Sum(nil))
	for _, tc := range []struct {
		secret, body, sig string
		want              bool
	}{
		{"k", "body", sig, true},
		{"k", "body!", sig, false},
		{"other", "body", sig, false},
		{"", "body", sig, false},
		{"k", "body", "not hex", false},
	} {
		if got := ValidHMACSHA256(tc.secret, []byte(tc.body), tc.sig); got != tc.want {
			t.Errorf("ValidHMACSHA256(%q, %q, %q) = %v", tc.secret, tc.body, tc.sig, got)
		}
	}
}

func TestEqual(t *testing.T) {
	if !Equal("a", "a") || Equal("a", "b") || Equal("", "") || Equal("a", "") {
		t.Error("Equal is wrong")
	}
}

// Main is run in a child process, where its exit is the process's: the
// child's first argument is a test flag, an unknown command, so it exits 2.
func TestMainExitsWithRunsCode(t *testing.T) {
	if os.Getenv("SDK_TEST_MAIN") == "1" {
		Main(testManifest, func(In) Result { return Accept() })
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainExitsWithRunsCode$")
	cmd.Env = append(os.Environ(), "SDK_TEST_MAIN=1")
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Errorf("err = %v, want exit 2", err)
	}
}
