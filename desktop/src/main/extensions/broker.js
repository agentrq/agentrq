// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The one place an extension reaches AgentRQ, and the only real boundary here.
 *
 * An extension is an MCP *client* — of the workspace server, of the supervisor,
 * or of neither. It never holds a credential: it asks, and this makes the call
 * with the token or OAuth session attached on the way out.
 *
 * That is the whole protection, and it is worth being precise about what it is
 * and is not. It does **not** contain the extension: this is trusted Node, it
 * can open its own sockets, and nothing here changes that. What it does is keep
 * the credential out of extension code — which matters because a workspace token
 * and a supervisor session outlive any single extension and reach every
 * workspace on the account. An extension that is compromised, or simply careless
 * with what it logs, cannot leak a key it was never given.
 *
 * ## Two questions, asked separately, on every call
 *
 * **May it call this tool?** Against the manifest's `mcp` allowlist, which the
 * user saw at install.
 *
 * **May it touch this workspace?** Against the grant, which is a different
 * thing: an extension allowed `getTask` is not thereby allowed it everywhere.
 *
 * ## `workspaceId` is addressing, not an argument
 *
 * An extension names the workspace it means, and on the **workspace** surface
 * that is all it is: the per-workspace server has one endpoint per workspace,
 * so the workspace is in the URL and the tools take no such parameter. Passing
 * it through was refused by the server outright —
 * `unexpected additional properties ["workspaceId"]` — which is a schema error
 * an extension author would have no way to explain, because they were doing the
 * only thing the broker's shape allowed.
 *
 * So it is read for the decision and the routing, then removed. On the
 * **supervisor** surface it stays, because there it genuinely is an argument:
 * one endpoint, and `listTasks(workspaceId)` means what it says.
 *
 * ## Supervisor access is not a third checkbox
 *
 * `listAllTasks` spans the platform and `createTask(workspaceId)` reaches
 * anywhere, so the supervisor surface *is* every workspace. Granting it and then
 * "limiting" the workspace list would be incoherent, and the ladder in
 * `grant.js` exists so that it cannot be expressed.
 */

/** The three rungs a grant can sit on. Ordered, because they escalate. */
export const SCOPE = {
  workspace: 'workspace',
  selected: 'selected',
  supervisor: 'supervisor',
}

const deny = (reason) => ({ ok: false, reason })

/** Everything but the addressing, for a server that already knows where it is. */
function withoutWorkspaceId(args) {
  const { workspaceId, ...rest } = args
  return rest
}

/**
 * Whether a grant permits one call.
 *
 * Split out from the calling because it is the rule, and a rule that can be
 * read on its own is one that can be checked on its own.
 */
export function permits(grant, { surface, tool, workspaceId }) {
  const allowed = grant?.tools?.[surface] ?? []
  if (!allowed.includes(tool)) {
    // Names the surface, because the same tool name can exist on both and
    // "not allowed" would leave an author guessing which one they asked for.
    return deny(`This extension may not call "${tool}" on the ${surface} server.`)
  }

  if (surface === 'supervisor') {
    // Reaching the supervisor at all is reaching every workspace; there is no
    // narrower state to check against.
    return grant.scope === SCOPE.supervisor
      ? { ok: true }
      : deny('This extension was not granted access to all workspaces.')
  }

  return permitsWorkspace(grant, workspaceId)
}

/**
 * Whether a grant reaches one workspace.
 *
 * Split out from `permits` because it is asked in a second place: the schedule
 * reconciler acts on the *host's* credential rather than the extension's — it
 * has to call tools no extension declares — so the tool allowlist does not
 * apply to it, and this is the half that still does. An extension granted one
 * workspace must not be able to declare standing work in another.
 */
export function permitsWorkspace(grant, workspaceId) {
  if (grant?.scope === SCOPE.supervisor) return { ok: true }

  if (!workspaceId) return deny('This call names no workspace.')

  const granted = grant?.workspaces ?? []
  if (granted.includes(workspaceId)) return { ok: true }

  // Told apart, because they are different situations for the person reading
  // it. "Not that one" means they picked workspaces and this is not among
  // them; an empty list means the grant reaches nothing at all, which is
  // somebody's mistake rather than their intent — and saying "not granted
  // access to that workspace" there sends them looking at the wrong thing.
  return granted.length === 0
    ? deny('This extension was not granted access to any workspace. Uninstall it and install it again, choosing the workspaces it may reach.')
    : deny('This extension was not granted access to that workspace.')
}

/**
 * @param {object} deps
 * @param {(args: object) => Promise<any>} deps.callWorkspace  Given { workspaceId, tool, args }.
 * @param {(args: object) => Promise<any>} deps.callSupervisor Given { tool, args }.
 * @param {(entry: object) => void} [deps.record]  Audit, for attribution.
 * @param {{warn: Function}} [deps.logger]
 */
export function createBroker({ callWorkspace, callSupervisor, record = () => {}, logger = console }) {
  /** @type {Map<string, object>} grants by extension name. */
  const grants = new Map()

  return {
    /** Remember what the user agreed to at the grant screen. */
    setGrant(name, grant) {
      grants.set(name, {
        scope: grant?.scope ?? SCOPE.workspace,
        workspaces: [...(grant?.workspaces ?? [])],
        tools: {
          workspace: [...(grant?.tools?.workspace ?? [])],
          supervisor: [...(grant?.tools?.supervisor ?? [])],
        },
        // Consent to be *asked* something, which is the other direction from
        // everything above and is enforced elsewhere — `tool-calls.js` reads it
        // from here. Copied rather than ignored because a grant that lost it on
        // the way through would be a consent the user gave and the app forgot,
        // and the only visible symptom would be an extension that never
        // reviews anything.
        hooks: { toolCall: String(grant?.hooks?.toolCall ?? 'none') },
      })
    },

    revoke(name) {
      grants.delete(name)
    },

    grantFor(name) {
      const grant = grants.get(name)
      return grant ? JSON.parse(JSON.stringify(grant)) : null
    },

    /**
     * The object an extension is handed.
     *
     * Closed over the extension's name, so it cannot ask on another's behalf,
     * and carrying no credential of any kind — that is the point of the whole
     * arrangement.
     */
    clientFor(name) {
      const call = async (surface, tool, args = {}) => {
        const grant = grants.get(name)
        if (!grant) return deny('This extension has not been granted any access.')

        const workspaceId = args.workspaceId ?? ''
        // Kept for the supervisor, where it is a real parameter; removed for the
        // workspace server, whose endpoint already names the workspace and whose
        // tools reject it. See the note at the top of this file.
        const toolArgs = surface === 'supervisor' ? args : withoutWorkspaceId(args)

        const allowed = permits(grant, { surface, tool, workspaceId })
        if (!allowed.ok) {
          // Recorded as well as refused: a refusal is the interesting half of an
          // audit trail, and it is what tells a user an extension is asking for
          // more than they gave it.
          record({ name, surface, tool, workspaceId, allowed: false, reason: allowed.reason })
          logger.warn?.(`[${name}] refused ${surface}.${tool}: ${allowed.reason}`)
          return allowed
        }

        record({ name, surface, tool, workspaceId, allowed: true })
        try {
          const result =
            surface === 'supervisor'
              ? await callSupervisor({ tool, args: toolArgs })
              : await callWorkspace({ workspaceId, tool, args: toolArgs })
          return { ok: true, result }
        } catch (error) {
          // The server's own message, not one invented here: an extension author
          // debugging a refused or malformed call needs what the server said.
          return deny(error?.message || 'The call failed.')
        }
      }

      return {
        workspace: (tool, args) => call('workspace', tool, args),
        supervisor: (tool, args) => call('supervisor', tool, args),
      }
    },
  }
}
