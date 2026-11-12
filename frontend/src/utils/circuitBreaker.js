// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Backs the app off a server that is overloaded or throttling it.
 *
 * Retrying into a struggling server makes it worse, and retrying into the
 * throttle is what earns an address the DDoS block. So after 3 failures in a
 * row, or at once on a 429, calls fail fast without touching the server for a
 * cooldown: 5s, doubling on each re-trip, at most 60s, unless the server's
 * Retry-After says otherwise. Then one probe call goes through; its success
 * closes the breaker, its failure re-opens it for longer.
 *
 * Only 429, 502/503/504 and network errors count. Any other status is the
 * server answering, so it is not a reason to stop asking.
 */

const THRESHOLD = 3;
const BASE_MS = 5000;
const MAX_MS = 60000;

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
  let probing = false;

  function trip(overrideMs) {
    trips += 1;
    failures = 0;
    const cooldown = overrideMs ?? Math.min(BASE_MS * 2 ** (trips - 1), MAX_MS);
    openUntil = now() + cooldown;
  }

  function close() {
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
      // After a trip the first call is the probe; the rest wait for it.
      if (wait > 0 || probing) return busyResponse(wait || BASE_MS);
      const probe = trips > 0;
      probing = probe;

      let res;
      try {
        res = await fetchImpl(input, init);
      } catch (err) {
        // A call the app cancelled says nothing about the server.
        if (err?.name !== 'AbortError') {
          if (probe) trip();
          else if (++failures >= THRESHOLD) trip();
        }
        throw err;
      } finally {
        if (probe) probing = false;
      }

      if (res.status === 429) {
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
