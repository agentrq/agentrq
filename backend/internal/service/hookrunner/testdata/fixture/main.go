// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The hook the runner's tests run, built for wasip1 by TestMain. Its "mode"
// input picks what it does with a request.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const manifest = `{"abiVersion":1,"name":"fixture","version":"1.0.0",
"inputs":[{"name":"secret","type":"string","secret":true,"required":true},{"name":"mode","type":"string"},{"name":"reply","type":"string"}],
"events":[{"name":"ci.failed"},{"name":"pr.opened"}]}`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "manifest" {
		fmt.Print(manifest)
		return
	}
	in, _ := io.ReadAll(os.Stdin)
	var req struct {
		Inputs map[string]string `json:"inputs"`
	}
	_ = json.Unmarshal(in, &req)
	switch req.Inputs["mode"] {
	case "reply":
		fmt.Print(req.Inputs["reply"])
	case "echo":
		// The input as the event's payload, so a test sees what was sent.
		out, _ := json.Marshal(map[string]any{"accept": true, "events": []any{
			map[string]any{"name": "ci.failed", "payload": string(in)},
		}})
		os.Stdout.Write(out)
	case "loop":
		for {
		}
	case "exit":
		os.Exit(3)
	case "flood":
		os.Stdout.Write([]byte(strings.Repeat("x", 5<<20)))
	case "memory":
		hog := make([][]byte, 0)
		for i := 0; i < 64; i++ {
			hog = append(hog, make([]byte, 1<<20))
		}
		fmt.Print(len(hog))
	}
}
