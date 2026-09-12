/**
 * Getting a supervisor credential, by asking the person in front of the app.
 *
 * The supervisor MCP server wants a token whose audience is `coremcp`, and the
 * only thing that mints one is the OAuth2 flow the backend already implements:
 * register a client, send the user to `/oauth2/authorize`, exchange the code at
 * `/oauth2/token`. Until now the desktop app simply did not walk it, so every
 * account-wide extension installed, loaded, and was told the supervisor was
 * unreachable.
 *
 * ## Registered dynamically, every time
 *
 * The backend supports RFC 7591 dynamic client registration, and the `client_id`
 * it returns is itself a signed credential carrying the redirect URIs it was
 * registered with — so `/oauth2/authorize` can bind the request's `redirect_uri`
 * to what this client actually registered rather than accepting any value.
 *
 * That makes registration cheap and disposable, which is what it should be
 * here: a desktop app is not a deployment with a client id to keep, and one
 * registered per authorisation cannot be a credential left lying around.
 *
 * ## Asked for, never assumed
 *
 * Nothing here runs on its own. An extension that wants the account has to be
 * installed with that grant, and the *user* is then asked — an app that
 * silently acquires an account-wide credential because something was installed
 * has taken a decision that was not its to take.
 *
 * ## The endpoints are asked for, not guessed
 *
 * The core MCP server is mounted under `/mcp`, and so are its OAuth endpoints
 * — except on an `mcp.<domain>` host, where they sit at the origin root. This
 * used to assume the root either way, which on an ordinary host reached no API
 * route at all and fell through to the SPA's GET-only catch-all: a POST to it
 * comes back `405`, and the person was told AgentRQ could not register. RFC
 * 8414 metadata already answers the question, so this asks and keeps the known
 * `/mcp/oauth2` layout only for a server that does not answer.
 *
 * ## The redirect comes back to the app, not to a page
 *
 * The window is watched for a navigation to the redirect URI and closed at that
 * point, rather than being pointed at something that renders. The code is in
 * the URL; there is nothing to display, and leaving a window open on a page
 * nobody needs is how people end up closing the wrong one.
 */

/** Where the authorisation lands. Matched exactly, and never followed. */
export const REDIRECT_URI = 'agentrq://extensions/authorized'

const fail = (reason) => ({ ok: false, reason })

/**
 * Reads the code out of the redirect, or says what went wrong instead.
 *
 * An OAuth error comes back as query parameters on the same URI, so this is
 * where a refusal arrives too — and a refusal is a person saying no, which is
 * a result rather than a failure.
 */
export function readRedirect(url, { state } = {}) {
  let parsed
  try {
    parsed = new URL(url)
  } catch {
    return fail('That was not a URL this could read.')
  }

  const params = parsed.searchParams
  const error = params.get('error')
  if (error) {
    return fail(params.get('error_description') || describeOAuthError(error))
  }

  // Checked before the code is worth anything: state is what ties this answer
  // to the request this app made, and an answer to somebody else's request is
  // not one to act on.
  const returned = params.get('state') ?? ''
  if (state && returned !== state) return fail('That authorisation did not match the one this app asked for.')

  const code = params.get('code') ?? ''
  return code ? { ok: true, code } : fail('The authorisation came back without a code.')
}

/** The errors RFC 6749 defines, in words rather than in slugs. */
function describeOAuthError(error) {
  if (error === 'access_denied') return 'You declined to authorise AgentRQ.'
  if (error === 'invalid_scope') return 'AgentRQ asked for something this server does not offer.'
  return `The server refused the authorisation: ${error}.`
}

/** Where the endpoints sit on a server that publishes no metadata. */
function defaultEndpoints(base) {
  return {
    registration: `${base}/mcp/oauth2/register`,
    authorization: `${base}/mcp/oauth2/authorize`,
    token: `${base}/mcp/oauth2/token`,
  }
}

/**
 * Metadata names its own endpoints; one naming somebody else's is not followed.
 *
 * The fetch here carries the session cookie, so an endpoint pointing off-origin
 * would be a document talking this app into spending that cookie elsewhere.
 */
function sameOrigin(base, candidate) {
  if (typeof candidate !== 'string' || !candidate) return ''
  try {
    return new URL(candidate).origin === new URL(base).origin ? candidate : ''
  } catch {
    return ''
  }
}

/**
 * @param {object} deps
 * @param {() => string} deps.serverUrl
 * @param {Function} deps.fetchImpl      Session-aware fetch, so the `at` cookie goes with it.
 * @param {(url: string, onRedirect: Function) => Promise<void>} deps.openWindow
 *        Shows the authorisation page and calls back on every navigation.
 * @param {() => string} [deps.randomState]
 */
export function createSupervisorAuth({ serverUrl, fetchImpl, openWindow, randomState = defaultState }) {
  /** The token, once somebody has agreed to it. Held in memory only. */
  let token = ''
  let refreshToken = ''

  /** What the server last said its endpoints were, and which server said it. */
  let discovered = null

  /** Reads RFC 8414 metadata, or answers with nothing and lets the caller cope. */
  async function fromMetadata(base) {
    try {
      const response = await fetchImpl(`${base}/.well-known/oauth-authorization-server`, {
        headers: { accept: 'application/json' },
      })
      if (!response.ok) return null

      const body = await response.json()
      const found = {
        registration: sameOrigin(base, body?.registration_endpoint),
        authorization: sameOrigin(base, body?.authorization_endpoint),
        token: sameOrigin(base, body?.token_endpoint),
      }
      // All three or none. Half a document read is a flow that registers in one
      // place and redeems the code in another, which fails later and less
      // legibly than simply using the layout this app already knows.
      return found.registration && found.authorization && found.token ? found : null
    } catch {
      return null
    }
  }

  /** The three endpoints for the configured server, asked for once per server. */
  async function endpoints() {
    const base = serverUrl().replace(/\/+$/, '')
    if (discovered?.base === base) return discovered.endpoints

    const resolved = (await fromMetadata(base)) ?? defaultEndpoints(base)
    discovered = { base, endpoints: resolved }
    return resolved
  }

  /** Registers a throwaway client and answers with its id. */
  async function register(endpoint) {
    const response = await fetchImpl(endpoint, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        client_name: 'AgentRQ Desktop Extensions',
        redirect_uris: [REDIRECT_URI],
        token_endpoint_auth_method: 'none',
        grant_types: ['authorization_code', 'refresh_token'],
        response_types: ['code'],
      }),
    })

    if (!response.ok) {
      throw new Error(`AgentRQ could not register with this server (${response.status}).`)
    }
    const body = await response.json()
    if (!body?.client_id) throw new Error('The server registered no client id.')
    return body.client_id
  }

  /** Exchanges the code for a token. */
  async function exchange(code, clientId, endpoint) {
    const response = await fetchImpl(endpoint, {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'authorization_code',
        code,
        client_id: clientId,
        redirect_uri: REDIRECT_URI,
      }).toString(),
    })

    if (!response.ok) {
      throw new Error(`AgentRQ could not complete the authorisation (${response.status}).`)
    }
    const body = await response.json()
    if (!body?.access_token) throw new Error('The server returned no access token.')
    return body
  }

  return {
    /** Whether the app holds a supervisor credential right now. */
    authorized: () => Boolean(token),
    token: () => token,

    /** Forget it. The next call asks again. */
    forget() {
      token = ''
      refreshToken = ''
    },

    /** Remember one read back from storage, without asking anybody. */
    restore(saved) {
      token = saved?.token ?? ''
      refreshToken = saved?.refreshToken ?? ''
    },

    /** What is worth saving, which is a credential and is treated as one. */
    saved: () => ({ token, refreshToken }),

    /**
     * Ask the user to authorise, and hold what comes back.
     *
     * Answers `{ ok: false, reason }` rather than throwing on every path: this
     * is reached from an extension asking for something, and the answer has to
     * be something the install screen or a panel can show.
     */
    async authorize() {
      if (!serverUrl()) return fail('No server is configured.')

      try {
        const where = await endpoints()
        const clientId = await register(where.registration)
        const state = randomState()

        const url = new URL(where.authorization)
        url.searchParams.set('client_id', clientId)
        url.searchParams.set('redirect_uri', REDIRECT_URI)
        url.searchParams.set('response_type', 'code')
        url.searchParams.set('state', state)

        // The window resolves with the redirect it was waiting for, or with
        // nothing if the person closed it — which is a decision, not an error.
        const redirect = await openWindow(url.toString(), REDIRECT_URI)
        if (!redirect) return fail('The authorisation window was closed.')

        const read = readRedirect(redirect, { state })
        if (!read.ok) return read

        const granted = await exchange(read.code, clientId, where.token)
        token = granted.access_token
        refreshToken = granted.refresh_token ?? ''
        return { ok: true }
      } catch (error) {
        return fail(error?.message || 'The authorisation could not be completed.')
      }
    },

    /**
     * Trade the refresh token for a new access token.
     *
     * Used when a call comes back unauthorised: the access token is short-lived
     * and the alternative is asking somebody to authorise again every time one
     * expires, which trains people to click through the screen that matters.
     */
    async refresh() {
      if (!refreshToken) return fail('There is nothing to refresh.')

      try {
        const { token: tokenEndpoint } = await endpoints()
        const response = await fetchImpl(tokenEndpoint, {
          method: 'POST',
          headers: { 'content-type': 'application/x-www-form-urlencoded' },
          body: new URLSearchParams({ grant_type: 'refresh_token', refresh_token: refreshToken }).toString(),
        })
        if (!response.ok) {
          // Gone rather than broken: a refused refresh means the session ended,
          // and holding a token that will not work only delays the question.
          this.forget()
          return fail('That authorisation has expired. Authorise AgentRQ again to use account-wide tools.')
        }
        const body = await response.json()
        if (!body?.access_token) {
          this.forget()
          return fail('The server returned no access token.')
        }
        token = body.access_token
        if (body.refresh_token) refreshToken = body.refresh_token
        return { ok: true }
      } catch (error) {
        return fail(error?.message || 'The authorisation could not be refreshed.')
      }
    },
  }
}

/** Enough entropy to tie one answer to one request. */
function defaultState() {
  return Array.from({ length: 4 }, () => Math.random().toString(36).slice(2)).join('')
}
