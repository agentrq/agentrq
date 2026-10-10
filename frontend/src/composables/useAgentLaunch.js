// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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

import { ref, computed, watch } from 'vue'
import * as api from '../api'
import { launchTerminalSize } from './useLaunchTerminalSize'
import { useAcpRegistry } from './useAcpRegistry'

/** The two things a daemon will run, and nothing else. */
export const KINDS = [
  {
    id: 'claude-code',
    label: 'Claude Code',
    description: 'Reads the workspace over MCP. The daemon writes its config.',
    needs: [],
    // Blank is Claude Code's own default for either, as it was before they
    // could be chosen here.
    optional: ['model', 'effort'],
  },
  {
    id: 'acp-gateway',
    label: 'ACP Gateway',
    description: 'Bridges another agent into the workspace.',
    // Only the agent picks which process runs at all. The model is the
    // gateway's own choice to make when nobody names one, so it is validated
    // when given but never required.
    needs: ['agent'],
    optional: ['model'],
  },
]

/**
 * Defaults for the gateway, taken from the repository's own `remote-agy`
 * target so the form opens on something that works rather than on an empty
 * required field.
 */
export const GATEWAY_DEFAULTS = { model: 'gemini-3.8-flash-high', agent: 'antigravity-acp' }

/**
 * The two sliders Claude Code launches with, each a list of steps from left to
 * right. The first step of each is blank, which is Claude Code's own default.
 *
 * The models are aliases, each following the latest model of its family, so
 * the list does not go stale with a release.
 */
export const CLAUDE_CODE_MODELS = [
  { id: '', name: 'Default' },
  { id: 'haiku', name: 'Haiku' },
  { id: 'sonnet', name: 'Sonnet' },
  { id: 'opus', name: 'Opus' },
  { id: 'fable', name: 'Fable' },
]

/** The levels `claude --effort` accepts, lowest first, after the default. */
export const CLAUDE_CODE_EFFORTS = [
  { id: '', name: 'Default' },
  { id: 'low', name: 'Low' },
  { id: 'medium', name: 'Medium' },
  { id: 'high', name: 'High' },
  { id: 'xhigh', name: 'Extra high' },
  { id: 'max', name: 'Max' },
]

/** The gateway's name for Claude, which it can ask for the models a machine's Claude offers. */
export const CLAUDE_ACP_AGENT = 'claude-acp'

/** The model families, fastest first, which is the order the model slider runs in. */
const CLAUDE_FAMILIES = ['haiku', 'sonnet', 'opus', 'fable']

/**
 * The model slider's steps from what a machine's Claude reported, or the fixed
 * list when it reported nothing.
 *
 * Only the aliases are kept: they follow the newest model of each family, and
 * the dated full ids beside them would crowd a slider past reading. Claude's
 * own `default` becomes the blank first step, named after what it stands for.
 */
export function claudeModelSteps(reported) {
  const models = (reported ?? []).filter((m) => typeof m?.id === 'string' && m.id)
  if (!models.length) return CLAUDE_CODE_MODELS
  const fallback = models.find((m) => m.id === 'default')
  const aliases = models
    .filter((m) => m.id !== 'default' && !m.id.startsWith('claude-'))
    .map((m) => ({ id: m.id, name: m.name || m.id }))
  const rank = (id) => {
    const i = CLAUDE_FAMILIES.indexOf(id)
    return i === -1 ? CLAUDE_FAMILIES.length : i
  }
  aliases.sort((a, b) => rank(a.id) - rank(b.id))
  const name = fallback?.description ? `Default (${fallback.description})` : 'Default'
  return [{ id: '', name }, ...aliases]
}

/** Where a value sits on a slider's steps; one that is not a step sits on the default. */
export function stepIndex(steps, value) {
  return Math.max(0, steps.findIndex((step) => step.id === value))
}

/**
 * Where the gateway's last agent and model are remembered.
 *
 * A per-browser convenience, not settings: never read by the server, and
 * nothing here is trusted for anything beyond pre-filling the two fields, the
 * same way a form remembers what somebody typed last time.
 */
const LAST_ACP_GATEWAY_KEY = 'agentrq:lastAcpGateway'

/**
 * The agent and model somebody last launched the gateway with, or null.
 *
 * Wrapped in a try/catch rather than assumed available: private browsing, a
 * blocked site data setting, or a full quota all throw here, and the correct
 * fallback for any of them is the same as having nothing stored — the form
 * opens on {@link GATEWAY_DEFAULTS} instead of failing to open at all.
 */
export function lastAcpGatewayChoice() {
  try {
    return acpGatewayParams(JSON.parse(localStorage.getItem(LAST_ACP_GATEWAY_KEY) ?? 'null'))
  } catch {
    return null
  }
}

/** A remembered gateway agent and model, or null when there is no agent to restore. */
function acpGatewayParams(parsed) {
  // The agent is the one field a launch cannot go without; a remembered
  // model is a bonus, not a condition for restoring the rest.
  if (!parsed?.agent) return null
  return { agent: parsed.agent, model: parsed.model ?? '' }
}

/** Remembers a gateway launch's agent and model for the next one. */
export function rememberAcpGatewayChoice({ agent, model }) {
  try {
    localStorage.setItem(LAST_ACP_GATEWAY_KEY, JSON.stringify({ agent, model: model ?? '' }))
  } catch {
    // Nothing to fall back to here: the next launch just opens on
    // GATEWAY_DEFAULTS again, exactly as it did before this existed.
  }
}

/** Where Claude Code's last model and effort are remembered, the same way as the gateway's. */
const LAST_CLAUDE_CODE_KEY = 'agentrq:lastClaudeCode'

/** The model and effort somebody last launched Claude Code with, or null. */
export function lastClaudeCodeChoice() {
  try {
    return claudeCodeParams(JSON.parse(localStorage.getItem(LAST_CLAUDE_CODE_KEY) ?? 'null'))
  } catch {
    return null
  }
}

/** A remembered Claude Code model and effort, or null. */
function claudeCodeParams(parsed) {
  if (!parsed || typeof parsed !== 'object') return null
  // Only what the sliders can show is restored; anything else opens on the
  // default rather than on a value no step stands for.
  return {
    model: CLAUDE_CODE_MODELS[stepIndex(CLAUDE_CODE_MODELS, parsed.model)].id,
    effort: CLAUDE_CODE_EFFORTS[stepIndex(CLAUDE_CODE_EFFORTS, parsed.effort)].id,
  }
}

/** Remembers a Claude Code launch's model and effort, blanks included, for the next one. */
export function rememberClaudeCodeChoice({ model, effort }) {
  try {
    localStorage.setItem(LAST_CLAUDE_CODE_KEY, JSON.stringify({ model: model ?? '', effort: effort ?? '' }))
  } catch {
    // The next launch opens on Claude Code's default instead.
  }
}

/**
 * The parameters a kind's fields open on: the workspace's last launch of that
 * kind, else the last launch of it anywhere, else its defaults.
 */
export function initialParams(kind, workspaceId) {
  const own = lastLaunchParams(workspaceId, kind)
  if (own) return own
  if (kind === 'acp-gateway') return lastAcpGatewayChoice() ?? { ...GATEWAY_DEFAULTS }
  return lastClaudeCodeChoice() ?? { model: '', effort: '' }
}

/** Remembers a launch's parameters for the next launch of the same kind. */
export function rememberParams(kind, extra) {
  if (kind === 'acp-gateway') rememberAcpGatewayChoice(extra)
  else rememberClaudeCodeChoice(extra)
}

/**
 * The fields for whichever kind is picked, each kind keeping its own.
 *
 * Both kinds have a model, and they are not interchangeable: a gateway model
 * handed to Claude Code is refused, so switching kind brings back what was
 * chosen for that kind rather than carrying the other's across.
 */
export function useKindParams(kind, getWorkspaceId = () => '') {
  let byKind = {}
  const params = ref(initialParams(kind.value, getWorkspaceId()))
  watch(
    kind,
    (next, prev) => {
      byKind[prev] = params.value
      params.value = byKind[next] ?? initialParams(next, getWorkspaceId())
    },
    { flush: 'sync' }
  )
  // Another workspace brings back its own last launch. One never launched
  // from leaves the fields as they are, rather than undoing what was typed.
  watch(
    getWorkspaceId,
    (workspaceId) => {
      byKind = {}
      const own = lastLaunchParams(workspaceId, kind.value)
      if (own) params.value = own
    },
    { flush: 'sync' }
  )
  return params
}

/**
 * Where each workspace's last launch is remembered: the machine, the kind,
 * and each kind's parameters (the gateway's agent and model, Claude Code's
 * model and effort).
 *
 * The same kind of per-browser convenience as the gateway's agent and model,
 * kept per workspace because "the machine this workspace runs on", and the
 * agent it runs, are facts about the workspace. Spin up reads it to start a
 * fork the way its parent was last started.
 */
const LAST_LAUNCH_KEY = 'agentrq:lastLaunch'

function readLaunches() {
  try {
    const parsed = JSON.parse(localStorage.getItem(LAST_LAUNCH_KEY) ?? 'null')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

/** The machine and kind this workspace was last launched with, or null. */
export function lastLaunchChoice(workspaceId) {
  const choice = readLaunches()[String(workspaceId)]
  if (!choice?.machineId || !KINDS.some((k) => k.id === choice.kind)) return null
  return { machineId: choice.machineId, kind: choice.kind }
}

/**
 * The parameters this workspace last launched `kind` with, or null when it
 * never has. Read through the same checks as the browser-wide choice.
 */
export function lastLaunchParams(workspaceId, kind) {
  if (!workspaceId) return null
  const params = readLaunches()[String(workspaceId)]?.params?.[kind]
  if (!params) return null
  return kind === 'acp-gateway' ? acpGatewayParams(params) : claudeCodeParams(params)
}

/**
 * Remembers a launch's machine and kind for its workspace, and the parameters
 * it was launched with, beside those of the workspace's other kind.
 */
export function rememberLaunchChoice(workspaceId, { machineId, kind, params }) {
  if (!workspaceId || !machineId) return
  try {
    const all = readLaunches()
    const key = String(workspaceId)
    const kept = all[key]?.params && typeof all[key].params === 'object' ? all[key].params : {}
    all[key] = { machineId, kind, params: params ? { ...kept, [kind]: params } : kept }
    localStorage.setItem(LAST_LAUNCH_KEY, JSON.stringify(all))
  } catch {
    // As with the gateway's choice: the next launch just asks again.
  }
}

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
export function workspaceEligibility(workspace, parent = null) {
  if (!workspace) return { ok: false, reason: 'No workspace selected.' }
  if (workspace.agentConnected) {
    return {
      ok: false,
      note: 'agent running',
      // Green, not amber: this is not a problem with the workspace, only a
      // reason *this* launch cannot start a second one.
      tone: 'good',
      reason: 'This workspace already has an agent connected.',
    }
  }
  if (workspace.forkOfId && !workspace.workingDirectory) return forkFolderEligibility(parent)
  if (!workspace.workingDirectory) {
    return {
      ok: false,
      note: 'no folder set',
      tone: 'warn',
      reason: 'This workspace has no working directory, so there is nowhere on the machine to run.',
      fix: { label: 'Set one in workspace settings', to: `/workspaces/${workspace.id}/settings` },
    }
  }
  return { ok: true }
}

/**
 * A fork with no folder yet: the daemon makes one from the parent's on the
 * first launch, so what matters is that the parent has a folder to make it
 * from. With the parent unknown the server decides, and says so if not.
 */
function forkFolderEligibility(parent) {
  if (parent && !parent.workingDirectory) {
    return {
      ok: false,
      note: 'no folder set',
      tone: 'warn',
      reason: `This fork's folder is made from ${parent.name}'s, and ${parent.name} has no working directory.`,
      fix: { label: `Set one in ${parent.name}'s settings`, to: `/workspaces/${parent.id}/settings` },
    }
  }
  return { ok: true, forkFrom: parent?.workingDirectory ?? '' }
}

/** What a launch says about where it runs; a fork with no folder yet says where one will be made from. */
export function launchFolderNote(workspace, parent = null) {
  if (workspace?.workingDirectory) return workspace.workingDirectory
  if (workspace?.forkOfId) {
    return parent?.workingDirectory ? `a folder will be made from ${parent.workingDirectory}` : 'a folder will be made for this fork'
  }
  return ''
}

/** The parent of a fork among these workspaces, or null. */
export function forkParent(workspace, workspaces) {
  if (!workspace?.forkOfId) return null
  return (workspaces ?? []).find((w) => String(w.id) === String(workspace.forkOfId)) ?? null
}

/**
 * The workspaces to offer, each with the one word that decides whether it is
 * worth picking.
 *
 * The page lists them rather than hiding them behind a dropdown, which means
 * the reason one of them will not work is visible before it is chosen instead
 * of after. The judgement is `workspaceEligibility`'s, not a second one: two
 * answers that disagreed would be worse than one.
 *
 * A ready workspace is noted with its folder, because that is the thing that
 * tells two similarly named workspaces apart on a machine. `tone` is a token
 * rather than a colour, the same way `sessionTone` is, so the view owns how
 * it looks and this stays testable without asserting a class name.
 */
export function workspaceOptions(workspaces) {
  return (workspaces ?? []).map((w) => {
    const parent = forkParent(w, workspaces)
    const eligibility = workspaceEligibility(w, parent)
    return {
      id: w.id,
      name: w.name,
      ready: eligibility.ok,
      note: eligibility.ok ? launchFolderNote(w, parent) : eligibility.note,
      tone: eligibility.ok ? null : eligibility.tone,
    }
  })
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

const BAD_SHAPE_REASON = (field) =>
  `That ${field} has characters the daemon will not accept: letters, digits, dot, dash and underscore, starting with a letter or digit.`

/** Whether the kind's own parameters are filled in and acceptable. */
export function paramsEligibility(kind, params) {
  const spec = KINDS.find((k) => k.id === kind)
  if (!spec) return { ok: false, reason: 'Pick what to run.' }
  for (const field of spec.needs) {
    const value = (params?.[field] ?? '').trim()
    if (!value) return { ok: false, reason: `${spec.label} needs a ${field}.` }
    if (!SAFE_PARAM.test(value)) return { ok: false, reason: BAD_SHAPE_REASON(field) }
  }
  // Optional fields are validated the same way when given, and skipped
  // entirely when not — leaving one blank is how "no preference" arrives.
  for (const field of spec.optional) {
    const value = (params?.[field] ?? '').trim()
    if (value && !SAFE_PARAM.test(value)) return { ok: false, reason: BAD_SHAPE_REASON(field) }
  }
  return { ok: true }
}

/**
 * The kind's own parameters shaped as the launch payload sends them: required
 * fields trimmed, and an optional one included only when it was actually
 * given. Shared by both launch composables so "which fields does a launch
 * send" has one answer.
 */
export function launchParamsPayload(kind, params) {
  const spec = KINDS.find((k) => k.id === kind)
  if (!spec) return {}
  const extra = {}
  for (const field of spec.needs) extra[field] = (params?.[field] ?? '').trim()
  for (const field of spec.optional) {
    const value = (params?.[field] ?? '').trim()
    if (value) extra[field] = value
  }
  return extra
}

/**
 * The agents the gateway's Agent field lists: the ACP registry's, then any
 * the machine's gateway reports that the registry does not, so an agent known
 * only to that machine is still offered. Each id once; first seen wins.
 */
export function mergeAcpAgents(registry = [], machine = []) {
  const seen = new Set()
  const out = []
  for (const a of [...registry, ...machine]) {
    if (!a?.id || seen.has(a.id)) continue
    seen.add(a.id)
    out.push(a)
  }
  return out
}

/**
 * The agents to show for what is typed in the Agent field: all of them while
 * it is empty or names one exactly, so a chosen agent can still be swapped
 * for another; otherwise those whose id, name or description contains it.
 */
export function filterAcpAgents(agents, query) {
  const q = (query ?? '').trim().toLowerCase()
  if (!q || agents.some((a) => a.id.toLowerCase() === q)) return agents
  return agents.filter((a) =>
    [a.id, a.name, a.description].some((s) => (s ?? '').toLowerCase().includes(q))
  )
}

/**
 * Suggestions for the gateway's Agent and Model fields, kept behind the two
 * launch composables so there is one place that decides when to ask rather
 * than two that could disagree.
 *
 * Both lookups are best-effort in the same way the eligibility checks above
 * are not: a machine that cannot be asked, or a workspace with no `.mcp.json`
 * on it yet, answers with nothing to suggest rather than an error, because the
 * plain text field behind this is always the real fallback.
 *
 * @param {object} deps
 * @param {import('vue').Ref<string>} deps.kind
 * @param {import('vue').Ref<{agent: string}>} deps.params
 * @param {() => string} deps.getMachineId current machine id, or falsy
 * @param {() => string} deps.getWorkspaceId current workspace id, or falsy —
 *        only needed for the model lookup, which the gateway can only answer
 *        from a workspace's own folder
 * @param {typeof api.fetchAcpAgents} [deps.fetchAcpAgents]
 * @param {typeof api.fetchAcpModels} [deps.fetchAcpModels]
 * @param {import('vue').Ref<object[]>} [deps.registryAgents] the ACP registry's agents
 * @param {typeof api.recordTelemetry} [deps.recordTelemetry]
 */
export function useAcpGatewaySuggestions({
  kind,
  params,
  getMachineId,
  getWorkspaceId,
  fetchAcpAgents = api.fetchAcpAgents,
  fetchAcpModels = api.fetchAcpModels,
  registryAgents = useAcpRegistry().agents,
  recordTelemetry = (...args) => api.recordTelemetry(...args),
}) {
  const machineAgents = ref([])
  const acpModels = ref([])
  const claudeReported = ref([])

  watch(
    () => (kind.value === 'acp-gateway' ? getMachineId() : ''),
    async (machineId) => {
      machineAgents.value = []
      if (!machineId) return
      try {
        const data = await fetchAcpAgents(machineId)
        machineAgents.value = data?.agents ?? []
      } catch {
        machineAgents.value = []
      }
    },
    { immediate: true }
  )
  const acpAgents = computed(() => mergeAcpAgents(registryAgents.value, machineAgents.value))

  /** Fills the Agent field from the list. */
  function pickAcpAgent(id) {
    params.value.agent = id
    recordTelemetry(api.TELEMETRY_UI_ACP_AGENT_PICK, getWorkspaceId())
  }

  watch(
    () => (kind.value === 'acp-gateway' ? (params.value.agent ?? '').trim() : ''),
    async (agent) => {
      acpModels.value = []
      const machineId = getMachineId()
      const workspaceId = getWorkspaceId()
      if (!agent || !machineId || !workspaceId) return
      try {
        const data = await fetchAcpModels(workspaceId, machineId, agent)
        acpModels.value = data?.models ?? []
      } catch {
        acpModels.value = []
      }
    }
  )

  // Claude Code's model slider, asked of the same machine through the gateway.
  // The folder is the workspace's for the same reason as the lookup above.
  watch(
    () => {
      if (kind.value !== 'claude-code') return ''
      const machineId = getMachineId()
      const workspaceId = getWorkspaceId()
      return machineId && workspaceId ? `${workspaceId}/${machineId}` : ''
    },
    async (key) => {
      claudeReported.value = []
      if (!key) return
      try {
        const data = await fetchAcpModels(getWorkspaceId(), getMachineId(), CLAUDE_ACP_AGENT)
        claudeReported.value = data?.models ?? []
      } catch {
        claudeReported.value = []
      }
    },
    { immediate: true }
  )
  const claudeModels = computed(() => claudeModelSteps(claudeReported.value))

  return { acpAgents, acpModels, claudeModels, pickAcpAgent }
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
    measureTerminalSize = launchTerminalSize,
    fetchAcpAgents,
    fetchAcpModels,
    registryAgents,
    recordTelemetry,
  } = deps

  const workspaces = ref([])
  const loading = ref(false)
  const error = ref('')
  const launching = ref(false)

  const workspaceId = ref('')
  const kind = ref(KINDS[0].id)
  const params = useKindParams(kind, () => workspaceId.value)

  const { acpAgents, acpModels, claudeModels, pickAcpAgent } = useAcpGatewaySuggestions({
    kind,
    params,
    getMachineId: () => machine?.value?.id,
    getWorkspaceId: () => workspaceId.value,
    ...(fetchAcpAgents ? { fetchAcpAgents } : {}),
    ...(fetchAcpModels ? { fetchAcpModels } : {}),
    ...(registryAgents ? { registryAgents } : {}),
    ...(recordTelemetry ? { recordTelemetry } : {}),
  })

  const selected = computed(() => workspaces.value.find((w) => w.id === workspaceId.value) ?? null)

  /** The same workspaces, as the page lists them. */
  const choices = computed(() => workspaceOptions(workspaces.value))

  /**
   * Every reason this launch would be refused, in the order they are worth
   * reading: the machine first, because nothing can run on a machine that is
   * off, whatever the workspace says.
   */
  const blockers = computed(() =>
    [
      machineEligibility(machine?.value),
      workspaceEligibility(selected.value, forkParent(selected.value, workspaces.value)),
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
      const extra = launchParamsPayload(kind.value, params.value)

      const { cols, rows } = await measureTerminalSize()
      const created = await launchAgent(workspaceId.value, {
        machineId: machine.value.id,
        kind: kind.value,
        cols,
        rows,
        ...extra,
      })
      rememberParams(kind.value, extra)
      rememberLaunchChoice(workspaceId.value, { machineId: machine.value.id, kind: kind.value, params: extra })
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
    choices,
    loading,
    error,
    launching,
    workspaceId,
    kind,
    params,
    selected,
    blockers,
    canLaunch,
    acpAgents,
    acpModels,
    claudeModels,
    pickAcpAgent,
    load,
    launch,
  }
}
