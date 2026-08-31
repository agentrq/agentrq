/**
 * A fetch that renews an expired session instead of dropping the user at the
 * login screen.
 *
 * The access token is short-lived by design; the refresh cookie behind it is
 * not. So a 401 is usually not "you are signed out", it is "that token aged
 * out" — worth one attempt to renew before believing it.
 *
 * Two rules do most of the work here:
 *
 * - **One refresh, not one per request.** The app fires several requests at
 *   once, so an expired token produces a burst of simultaneous 401s. Without
 *   sharing the attempt, each would start its own refresh, and each would
 *   rotate the cookie out from under the others — turning one expiry into a
 *   race that can sign the user out.
 * - **Exactly one retry.** If the replay also fails, that is a real 401 and it
 *   is passed through. Retrying again would loop against a server that is
 *   simply saying no.
 */

/**
 * @param {object} deps
 * @param {typeof fetch} deps.fetchImpl
 * @param {string} deps.refreshUrl  the endpoint that swaps a refresh cookie for a session
 */
export function createAuthedFetch({ fetchImpl, refreshUrl }) {
  /** The refresh in progress, shared by everyone who hit a 401 at once. */
  let inFlight = null;

  function refreshOnce() {
    if (!inFlight) {
      inFlight = fetchImpl(refreshUrl, { method: 'POST' })
        .then((res) => Boolean(res?.ok))
        // A refresh that cannot even be attempted is a failed refresh, not an
        // error to propagate: the caller still has its original 401 to return.
        .catch(() => false)
        .finally(() => {
          inFlight = null;
        });
    }
    return inFlight;
  }

  return async function authedFetch(input, init) {
    const res = await fetchImpl(input, init);
    if (res?.status !== 401) return res;

    // Refreshing the refresh call would be a loop with extra steps.
    if (input === refreshUrl) return res;

    const renewed = await refreshOnce();
    if (!renewed) return res;

    return fetchImpl(input, init);
  };
}
