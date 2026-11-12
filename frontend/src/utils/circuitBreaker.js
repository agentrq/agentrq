// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Backs the app off a server that is overloaded or throttling it.
 *
 * Retrying into a struggling server makes it worse, and retrying into the
 * throttle is what earns an address the DDoS block. So after 3 failures in a
 * row, or at once on a throttle's 429, calls fail fast without touching the
 * server for a cooldown: 5s, doubling on each re-trip, at most 60s, unless the
 * server's Retry-After says otherwise. Then one probe call goes through; its
 * success closes the breaker, its failure re-opens it for longer. A slow probe
 * holds the others back for 5s at most; after that the next call probes too.
 *
 * Only 502/503/504, network errors and a 429 with Retry-After count: that 429
 * is the address throttle's. A 429 without one is a per-user quota on one kind
 * of call, and any other status is the server answering, so neither is a
 * reason to stop asking.
 *
 * An answer to a call sent before the breaker last tripped or closed is
 * ignored: it reports on the server as it was then, so it must not close a
 * fresh trip, nor double the cooldown again for the same overload.
 */

const THRESHOLD = 3;
const BASE_MS = 5000;
const MAX_MS = 60000;
const PROBE_HOLD_MS = 5000;

const OVERLOAD = new Set([502, 503, 504]);

/** Retry-After in ms, from seconds or an HTTP date; null when absent or unreadable. */
function retryAfterMs(res, now) {
  const value = res.headers?.get?.('Retry-After');
  if (!value) return null;
  const seconds = Number(value);
  if (Number.isFinite(seconds)) return Math.max(0, seconds * 1000);
  const at = Date.parse(value);
  return Number.isNaN(at) ? null : Math.max(0, at - now);
}

/**
 * The answer a call gets while the breaker is open, shaped like the API's own
 * errors so call sites treat it as the server being unavailable.
 */
function busyResponse(waitMs) {
  return new Response(
    JSON.stringify({ error: { message: 'server is busy, try again shortly', code: 503 } }),
    {
      status: 503,
      headers: {
        'Content-Type': 'application/json',
        'Retry-After': String(Math.ceil(waitMs / 1000)),
      },
    },
  );
}

/** @param {{ now?: () => number }} [deps] */
export function createCircuitBreaker({ now = () => Date.now() } = {}) {
  let failures = 0;
  let trips = 0;
  let openUntil = 0;
  let generation = 0;
  let probes = 0;
  let probeHeldUntil = 0;

  // A trip or a close starts a new generation, and drops the hold of a probe
  // from the last one.
  function trip(overrideMs) {
    generation += 1;
    probeHeldUntil = 0;
    trips += 1;
    failures = 0;
    const cooldown = overrideMs ?? Math.min(BASE_MS * 2 ** (trips - 1), MAX_MS);
    openUntil = now() + cooldown;
  }

  function close() {
    if (trips > 0) generation += 1;
    probeHeldUntil = 0;
    failures = 0;
    trips = 0;
    openUntil = 0;
  }

  /** How long until a call may reach the server; 0 when it may now. */
  function retryIn() {
    return Math.max(0, openUntil - now());
  }

  /** @param {typeof fetch} fetchImpl */
  function wrap(fetchImpl) {
    return async function breakerFetch(input, init) {
      const wait = retryIn();
      const held = probeHeldUntil - now();
      // After a trip the first call is the probe; the rest wait for it.
      if (wait > 0 || held > 0) return busyResponse(Math.max(wait, held));
      const sent = generation;
      const probe = trips > 0;
      const mine = probe ? ++probes : 0;
      if (probe) probeHeldUntil = now() + PROBE_HOLD_MS;

      let res;
      try {
        res = await fetchImpl(input, init);
      } catch (err) {
        // A call the app cancelled says nothing about the server.
        if (err?.name !== 'AbortError' && sent === generation) {
          if (probe) trip();
          else if (++failures >= THRESHOLD) trip();
        }
        throw err;
      } finally {
        if (mine === probes) probeHeldUntil = 0;
      }

      if (sent !== generation) return res;
      if (res.status === 429 && res.headers?.get?.('Retry-After')) {
        trip(retryAfterMs(res, now()));
      } else if (OVERLOAD.has(res.status)) {
        if (probe || ++failures >= THRESHOLD) trip(retryAfterMs(res, now()));
      } else {
        close();
      }
      return res;
    };
  }

  return { wrap, retryIn };
}

/** The one breaker for the app's API traffic, shared by every call and the event stream. */
export const apiBreaker = createCircuitBreaker();
