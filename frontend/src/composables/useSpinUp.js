// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Spin up: one task, handed to an agent of its own.
 *
 * Three calls the interface already makes — fork the workspace, move the task
 * into the fork, launch an agent there — composed here in the browser rather
 * than behind a new endpoint. They are not a transaction and do not pretend to
 * be: a failure part way leaves the fork (useful by itself; the task can be
 * moved by hand) and says which step failed.
 */

import { computed, reactive, ref } from 'vue'
import * as api from '../api'
import { TELEMETRY_UI_SPIN_UP } from '../api'
import {
  KINDS,
  lastLaunchChoice,
  launchParamsPayload,
  paramsEligibility,
  rememberParams,
  rememberLaunchChoice,
  useAcpGatewaySuggestions,
  useKindParams,
} from './useAgentLaunch'
import { launchableMachines, machineChoiceEligibility } from './useWorkspaceAgentLaunch'
import { canFork, kebabName } from './useWorkspaceForks'
import { launchTerminalSize } from './useLaunchTerminalSize'

/**
 * A task still waiting for an agent. An ongoing one already has one, here, and
 * moving it into a fork would take it from under that agent; a schedule is a
 * template, not work to hand off.
 */
const WAITING = ['notstarted', 'blocked']

/** Whether a task row offers Spin up: a task waiting for an agent, in a workspace that can be forked. */
export function canSpinUp(task, workspace) {
  return !!task && WAITING.includes(task.status) && canFork(workspace)
}

/** The fork is named after the task, in kebab-case, cut to what a workspace name may hold. */
export function spinUpName(task) {
  return kebabName(task?.title) || 'spin-up'
}

/** What each step is called when it fails. */
export const STEP_LABELS = {
  fork: 'fork the workspace',
  move: 'move the task into the fork',
  launch: 'start the agent in the fork',
}

/** The message for a spin up that stopped part way. */
export function spinUpFailure(step, err, forkName) {
  const why = err?.message || 'unknown error'
  if (step === 'fork') return `Could not ${STEP_LABELS.fork}: ${why}`
  return `Forked ${forkName}, but could not ${STEP_LABELS[step]}: ${why}`
}

/**
 * The popover's state and the run.
 *
 * @param {object} deps the API functions, replaceable in tests
 */
export function useSpinUp(deps = {}) {
  const {
    fetchMachines = api.fetchMachines,
    forkWorkspace = api.forkWorkspace,
    moveTask = api.moveTask,
    launchAgent = api.launchAgent,
    recordTelemetry = api.recordTelemetry,
    measureTerminalSize = launchTerminalSize,
    fetchAcpAgents,
    fetchAcpModels,
    registryAgents,
  } = deps

  const state = reactive({ task: null, workspace: null, x: 0, y: 0 })
  const machines = ref([])
  const machineId = ref('')
  const kind = ref(KINDS[0].id)
  const params = useKindParams(kind, () => state.workspace?.id)
  const running = ref(false)
  const step = ref('')
  const error = ref('')
  // The fork a spin up made before it stopped part way. Running again would
  // make a second one, so the popover says what happened and offers no retry.
  const forked = ref(null)

  const { acpAgents, acpModels, claudeModels, pickAcpAgent } = useAcpGatewaySuggestions({
    kind,
    params,
    getMachineId: () => machineId.value,
    // The fork does not exist yet; its folder will be the parent's, so the
    // parent is what the gateway can answer for.
    getWorkspaceId: () => state.workspace?.id,
    ...(fetchAcpAgents ? { fetchAcpAgents } : {}),
    ...(fetchAcpModels ? { fetchAcpModels } : {}),
    ...(registryAgents ? { registryAgents } : {}),
    recordTelemetry,
  })

  const available = computed(() => launchableMachines(machines.value))

  /**
   * Every reason this would be refused before anything is made. The parent's
   * folder is among them: a fork's folder is made from it, and forking first
   * only to have the launch refused would leave a fork nobody asked for.
   */
  const blockers = computed(() => {
    const ws = state.workspace
    const folder = ws && !ws.workingDirectory
      ? {
          ok: false,
          reason: `${ws.name} has no working directory, so there is no folder to make the fork's from.`,
          fix: { label: 'Set one in workspace settings', to: `/workspaces/${ws.id}/settings` },
        }
      : { ok: true }
    // The task can be taken on while the popover is open.
    const waiting = state.task?.status === 'ongoing'
      ? { ok: false, reason: 'An agent has already started on this task, so it stays here.' }
      : { ok: true }
    return [waiting, machineChoiceEligibility(machineId.value, machines.value), folder, paramsEligibility(kind.value, params.value)]
      .filter((e) => !e.ok)
  })

  const canRun = computed(() => !!state.task && !forked.value && blockers.value.length === 0 && !running.value)

  /**
   * Opens for one task, on the parent's last launch where that machine is
   * still online, and on the only machine when there is one.
   */
  async function open(task, workspace, at = { x: 0, y: 0 }) {
    Object.assign(state, { task, workspace, x: at.x, y: at.y })
    error.value = ''
    step.value = ''
    forked.value = null
    try {
      const data = await fetchMachines()
      machines.value = data?.machines ?? []
    } catch (e) {
      machines.value = []
      error.value = e?.message || 'Failed to load machines'
    }
    const online = launchableMachines(machines.value)
    const last = lastLaunchChoice(workspace?.id)
    if (last && online.some((m) => m.id === last.machineId)) {
      machineId.value = last.machineId
      kind.value = last.kind
    } else {
      machineId.value = online.length === 1 ? online[0].id : ''
    }
  }

  function close() {
    state.task = null
  }

  /**
   * Forks, moves, launches. Resolves to `{ task, fork, session }` on success,
   * and to `{ task, fork, failedStep }` when it stopped part way (`fork` null if the fork
   * itself failed), with `error` saying which step and why.
   */
  async function run() {
    if (!canRun.value) return null
    const { task, workspace } = state
    const name = spinUpName(task)
    running.value = true
    error.value = ''
    let fork = null
    try {
      step.value = 'fork'
      fork = (await forkWorkspace(workspace.id, { name }))?.workspace ?? null
      step.value = 'move'
      await moveTask(workspace.id, task.id, fork.id)
      step.value = 'launch'
      const extra = launchParamsPayload(kind.value, params.value)
      const { cols, rows } = await measureTerminalSize()
      const created = await launchAgent(fork.id, { machineId: machineId.value, kind: kind.value, cols, rows, ...extra })
      rememberParams(kind.value, extra)
      // Remembered for the parent: the next spin up there starts the same way.
      rememberLaunchChoice(workspace.id, { machineId: machineId.value, kind: kind.value, params: extra })
      rememberLaunchChoice(fork.id, { machineId: machineId.value, kind: kind.value, params: extra })
      recordTelemetry(TELEMETRY_UI_SPIN_UP, workspace.id)
      state.task = null
      return { task, fork, session: created?.session ?? null }
    } catch (e) {
      error.value = spinUpFailure(step.value, e, name)
      forked.value = fork
      return { task, fork, failedStep: step.value }
    } finally {
      running.value = false
    }
  }

  return {
    state,
    machines,
    available,
    machineId,
    kind,
    params,
    running,
    step,
    error,
    forked,
    blockers,
    canRun,
    acpAgents,
    acpModels,
    claudeModels,
    pickAcpAgent,
    open,
    close,
    run,
  }
}
