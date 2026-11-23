// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Noticing that Chromium's network service has died.
 *
 * It is a separate utility process, and when it goes every request that was in
 * flight is abandoned rather than failed — no rejection, no error event, just
 * silence. Electron restarts the service, so the app keeps working for anything
 * started afterwards, and that is what makes this worth handling explicitly:
 * the symptom is not a crash but a connection that is dead and still looks
 * open. The main process's event stream is the one thing that holds such a
 * connection for the life of the app, so it is what has to be told.
 *
 * Everything below is a plain function over the details Electron passes, so it
 * is testable without an Electron binary.
 */

/** What Electron calls the network service in `child-process-gone` details. */
export const NETWORK_SERVICE = 'network.mojom.NetworkService'

/**
 * Whether these `child-process-gone` details describe the network service.
 *
 * Matched on the service name and not the process type alone: a GPU or an
 * extension utility process dying is a different event with a different answer,
 * and reconnecting the event stream for either would be noise.
 */
export function isNetworkServiceGone(details) {
  return details?.type === 'Utility' && details?.serviceName === NETWORK_SERVICE
}

/** A one-line description of a departed child process, for the log. */
export function describeProcessGone(details) {
  const name = details?.serviceName || details?.name || details?.type || 'unknown'
  const reason = details?.reason || 'unknown'
  const code = details?.exitCode
  return code === undefined ? `${name} (${reason})` : `${name} (${reason}, exit ${code})`
}

/**
 * The `child-process-gone` listener.
 *
 * @param {object} deps
 * @param {() => void} deps.restartEventStream  drop the held connection and reconnect
 * @param {(message: string) => void} [deps.log]
 * @returns {(details: object) => boolean} true when the network service was the
 *          process that went, which is the only case acted on.
 */
export function createProcessGoneHandler({ restartEventStream, log = () => {} }) {
  return function handleChildProcessGone(details) {
    const what = describeProcessGone(details)
    if (!isNetworkServiceGone(details)) {
      log(`child process gone: ${what}`)
      return false
    }

    // Electron brings the service back by itself; what it cannot do is tell the
    // stream that the socket it is still reading from no longer exists.
    log(`network service gone: ${what} — reconnecting the event stream`)
    restartEventStream()
    return true
  }
}
