# Hooks

> Read before changing hooks, their registrations or deliveries.

A request from an outside service fires one of the account's events. A **hook**
is a WebAssembly module that validates the request and writes the events; a
**registration** is one use of a hook in an account, with its own id and URL.
Built-in hooks (`user_id` 0) are the root user's; an account's own hooks run
only while `hooks.allowUserHooks` is on.

- **The URL's id is a monoflake, so it is guessable.** The credentials are the
  registration's `agentrq_hook_token` and the hook's own check of a secret
  input, which every hook must declare. Never let a hook without one register.
- **The token is checked against a fresh read of the registration, never a
  cache.** A rotated token has to stop working on every instance at once.
- **A request is saved and answered 202 before any hook runs.** The provider
  never learns that a signature was wrong; the delivery log says so.
- **A queued row's `next_attempt_at` is also its lease.** A claim moves it to the
  lease's end, so a crashed worker's row is claimed again by the same query.
- **The raw request is cleared once a delivery is processed**, except a failed
  one, which keeps it until the retention passes so it can be replayed.
- **The validation lives in the hook, not the server.** A new signing scheme is
  a new hook; never teach the server a provider's format.
- **A hook runs in a fresh instance for every request**, with no filesystem,
  network or environment; only its compiled form is cached, by digest. Reusing
  an instance would let one request, or one account, read another's.
- **An `*OutputError` is final, any other run error is retried.** A hook is
  deterministic, so a wrong answer would come back the same; a timeout or a trap
  may not.
