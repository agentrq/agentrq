// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package packaging holds the service definitions shipped in the release
// archives. It has no code; these tests keep the files themselves honest.
package packaging

import (
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// programArguments reads the LaunchAgent the way launchd would, strictly: a
// file encoding/xml rejects (a "--" in a comment, say) is one launchd will not
// load either.
func programArguments(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("com.agentrq.agentrqd.plist")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	var args []string
	var key, inArray, inString bool
	var lastKey string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the plist is not well-formed XML: %v", err)
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			key = tok.Name.Local == "key"
			inArray = inArray || (tok.Name.Local == "array" && lastKey == "ProgramArguments")
			inString = tok.Name.Local == "string"
		case xml.EndElement:
			if tok.Name.Local == "array" {
				inArray = false
			}
			key, inString = false, false
		case xml.CharData:
			switch {
			case key:
				lastKey = string(tok)
			case inArray && inString:
				args = append(args, string(tok))
			}
		}
	}
	if len(args) == 0 {
		t.Fatal("no ProgramArguments in the plist")
	}
	return args
}

// launchd expands neither ~ nor $HOME, so a literal home folder in the
// shipped file is right for one user and silently never starts for the rest.
func TestTheLaunchAgentNamesNobodysHomeFolder(t *testing.T) {
	for _, a := range programArguments(t) {
		if strings.Contains(a, "/Users/") || strings.HasPrefix(a, "~") {
			t.Errorf("ProgramArguments holds %q, which launchd will not expand", a)
		}
	}
}

// Run as launchd runs it: the first argument is the program, nothing is
// expanded, and HOME is the user's.
func TestTheLaunchAgentStartsTheDaemonFromHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a LaunchAgent runs under sh")
	}
	args := programArguments(t)
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\necho \"started: $*\"\n"
	if err := os.WriteFile(filepath.Join(bin, "agentrqd"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "started: serve" {
		t.Errorf("got %q, want the daemon started with serve", got)
	}
}
