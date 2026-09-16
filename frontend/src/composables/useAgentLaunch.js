// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Starting an agent on a machine, from the control panel.
 *
 * The backend refuses a launch for five different reasons, each with its own
 * status: the workspace already has an agent, its folder is not set, the
 * machine is disabled, the machine is not connected, or this server does not
 * hold its socket. A form that fired and then reported whichever it hit would
 * be technically honest and useless — you would press the button to find out
 * whether you could press the button.
 *
 * So eligibility is computed here, before anything is sent, and shown. The
 * launch itself still handles every refusal, because the answer can change
 * between the page loading and the button being pressed.
 */

import { ref, computed } from 'vue'
import * as api from '../api'

/** The two things a daemon will run, and nothing else. */
export const KINDS = [
  {
    id: 'claude-code',
    label: 'Claude Code',
    description: 'Reads the workspace over MCP. The daemon writes its config.',
    needs: [],
  },
  {
    id: 'acp-gateway',
    label: 'ACP Gateway',
    description: 'Bridges another agent into the workspace.',
    needs: ['model', 'agent'],
  },
]

/**
 * Defaults for the gateway, taken from the repository's own `remote-agy`
 * target so the form opens on something that works rather than on two empty
 * required fields.
 */
export const GATEWAY_DEFAULTS = { model: 'gemini-3.8-flash-high', agent: 'antigravity-acp' }

/**
 * The terminal a session starts with.
 *
 * Large enough that an agent's first output is not immediately wrapped, and
 * corrected by the real terminal the moment somebody attaches — the browser
 * sends its actual size on every connection.
 */
export const INITIAL_COLS = 120
export const INITIAL_ROWS = 40

/**
 * What the daemon accepts as a model or agent name.
 *
 * Mirrors `safeParam` in the supervisor, which refuses anything that could
 * arrive as a flag or a path. Checked here so the refusal arrives while
 * somebody is still looking at the field rather than after a round trip.
 */
const SAFE_PARAM = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/

/**
 * Whether an agent can be started for this workspace, and if not, why.
 *
 * The reason is the useful half. "Cannot launch" tells somebody they are
 * stuck; "this workspace has no working directory set" tells them what to do,
 * which is why `fix` names the page that fixes it.
 */
export function workspaceEligibility(workspace) {
  if (!workspace) return { ok: false, reason: 'No workspace selected.' }
  if (workspace.agentConnected) {
    return {
      ok: false,
      reason: 'This workspace already has an agent connected.',
    }
  }
  if (!workspace.workingDirectory) {
    return {
      ok: false,
      reason: 'This workspace has no working directory, so there is nowhere on the machine to run.',
      fix: { label: 'Set one in workspace settings', to: `/workspaces/${workspace.id}/settings` },
    }
  }
  return { ok: true }
}

/**
 * Whether this machine is already running something for that workspace.
 *
 * Partial knowledge, deliberately: this page only knows its own machine's
 * sessions, and an agent for the same workspace could be running on another
 * one. So this catches the common case — launching, then pressing again on the
 * page you are already looking at — and never claims the opposite. The
 * server's own gate is what actually decides, because two people can press at
 * the same moment and only it sees both.
 */
export function sessionEligibility(workspaceId, sessions) {
  const live = (sessions ?? []).find(
    (s) => s.workspaceId === workspaceId && (s.status === 'starting' || s.status === 'running')
  )
  if (!live) return { ok: true }
  return {
    ok: false,
    reason: `This machine is already running a ${live.kind ?? 'agent'} for that workspace.`,
  }
}

/**
 * Whether the machine itself can take a session.
 *
 * Separate from the workspace's eligibility because it is a different
 * problem with a different fix, and conflating them would tell somebody to
 * change a workspace setting when the machine is simply switched off.
 */
export function machineEligibility(machine) {
  if (!machine) return { ok: false, reason: 'Loading…' }
  if (!machine.enabled) {
    return { ok: false, reason: 'This machine is disabled. Enable it below to run agents on it.' }
  }
  if (!machine.online) {
    return {
      ok: false,
      reason: 'This machine is offline. Start agentrqd on it, and it will reconnect on its own.',
    }
  }
  return { ok: true }
}

/** Whether the kind's own parameters are filled in and acceptable. */
export function paramsEligibility(kind, params) {
  const spec = KINDS.find((k) => k.id === kind)
  if (!spec) return { ok: false, reason: 'Pick what to run.' }
  for (const field of spec.needs) {
    const value = (params?.[field] ?? '').trim()
    if (!value) return { ok: false, reason: `${spec.label} needs a ${field}.` }
    if (!SAFE_PARAM.test(value)) {
      return {
        ok: false,
        reason: `That ${field} has characters the daemon will not accept: letters, digits, dot, dash and underscore, starting with a letter or digit.`,
      }
    }
  }
  return { ok: true }
}

/**
 * The launcher for one machine.
 *
 * @param {object} deps `machine` (a ref) plus, in tests, the API functions
 */
export function useAgentLaunch(deps = {}) {
  const {
    machine,
    // The machine's own sessions, so the page can answer for itself what it
    // already knows rather than making the server say no.
    sessions,
    fetchWorkspaces = api.fetchWorkspaces,
    launchAgent = api.launchAgent,
  } = deps

  const workspaces = ref([])
  const loading = ref(false)
  const error = ref('')
  const launching = ref(false)

  const workspaceId = ref('')
  const kind = ref(KINDS[0].id)
  const params = ref({ ...GATEWAY_DEFAULTS })

  const selected = computed(() => workspaces.value.find((w) => w.id === workspaceId.value) ?? null)

  /**
   * Every reason this launch would be refused, in the order they are worth
   * reading: the machine first, because nothing can run on a machine that is
   * off, whatever the workspace says.
   */
  const blockers = computed(() =>
    [
      machineEligibility(machine?.value),
      workspaceEligibility(selected.value),
      sessionEligibility(workspaceId.value, sessions?.value),
      paramsEligibility(kind.value, params.value),
    ].filter((e) => !e.ok)
  )

  const canLaunch = computed(() => blockers.value.length === 0 && !launching.value)

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const data = await fetchWorkspaces()
      // Archived workspaces are left out: they are not somewhere anybody
      // means to start new work.
      workspaces.value = (data?.workspaces ?? []).filter((w) => !w.archivedAt)
    } catch (e) {
      error.value = e?.message || 'Failed to load workspaces'
    } finally {
      loading.value = false
    }
  }

  /**
   * Ask the machine to start an agent.
   *
   * Resolving means the daemon has been *asked*. The session reports its own
   * state over the event stream, so a caller that treated this as "running"
   * would be claiming something it cannot know — the folder might not exist on
   * that machine, and only the daemon can find that out.
   */
  async function launch() {
    if (!canLaunch.value) return null
    launching.value = true
    error.value = ''
    try {
      const spec = KINDS.find((k) => k.id === kind.value)
      const extra = {}
      for (const field of spec.needs) extra[field] = params.value[field].trim()

      const created = await launchAgent(workspaceId.value, {
        machineId: machine.value.id,
        kind: kind.value,
        cols: INITIAL_COLS,
        rows: INITIAL_ROWS,
        ...extra,
      })
      return created?.session ?? null
    } catch (e) {
      error.value = e?.message || 'Failed to start the agent'
      return null
    } finally {
      launching.value = false
    }
  }

  return {
    workspaces,
    loading,
    error,
    launching,
    workspaceId,
    kind,
    params,
    selected,
    blockers,
    canLaunch,
    load,
    launch,
  }
}
