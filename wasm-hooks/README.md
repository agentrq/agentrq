# Built-in hooks

An AgentRQ hook turns a request from an outside service, such as a failed CI run on GitHub, into the account's events. It is a WebAssembly program the server runs in a sandbox: no files, no network, no environment, 32 MiB of memory and 250 ms per request.

- `generic/` checks a shared token in a header and passes the body through as the event's payload.
- `github/` checks `X-Hub-Signature-256` and writes `ci.failed`, `ci.succeeded`, `pr.opened`, `pr.review_requested`, `issue.opened`, `release.published` and `push` events.
- `sdk/` is the contract for hooks written in Go.

`make wasm-hooks` builds them into `wasm-hooks/dist/`. Each release also ships them as `agentrq-hook-<name>_<version>.wasm`, for the root user to register.

## Writing one

A hook is a WASI preview1 command, in any language that targets `wasip1`.

- Run with `manifest`, it prints its manifest as JSON: name, version, inputs and events. It must declare at least one required secret input, and check it.
- Run with `handle`, it reads `{registrationId, request, inputs, now}` on stdin and prints `{accept, reason, events}`. Each event has a `name` the manifest declares, and optionally a `dedupKey`, a `payload` (a string, or any JSON value) and a `faq`.

In Go:

```go
func main() {
	sdk.Main(manifest, func(in sdk.In) sdk.Result {
		if !sdk.Equal(in.String("token"), in.Request.Header("X-Token")) {
			return sdk.Reject("wrong token")
		}
		return sdk.Accept(sdk.Out{Name: "deploy.done", Payload: string(in.Request.Body)})
	})
}
```

Build it with `GOOS=wasip1 GOARCH=wasm go build -o hook.wasm .`.

Licensed under the Apache License 2.0; see `LICENSE`.
