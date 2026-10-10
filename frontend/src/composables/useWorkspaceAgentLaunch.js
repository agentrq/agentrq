// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Starting an agent from the workspace, rather than from the machine.
 *
 * `useAgentLaunch` answers "this machine is open — which workspace should it
 * run?". This is the same launch from the other end: the workspace is open and
 * offline, and the question is which machine should run it. The pairing is the
 * same one either way, so the eligibility rules are imported rather than
 * rewritten — two copies of "why can't I launch" would eventually disagree,
 * and the one somebody is reading would be the wrong one.
 *
 * What it deliberately cannot answer is whether an agent is already starting
 * elsewhere: this end knows the workspace, not every machine's sessions, and
 * the window between "launched" and "connected" is invisible from here. The
 * server closes that window (`ActiveSessionForWorkspace`), and a refusal from
 * it is shown as it arrives.
 */

import { ref, computed } from 'vue'
import * as api from '../api'
import {
  KINDS,
  launchParamsPayload,
  launchStart,
  paramsEligibility,
  rememberParams,
  rememberLaunchChoice,
  useAcpGatewaySuggestions,
  useKindParams,
  workspaceEligibility,
} from './useAgentLaunch'
import { launchTerminalSize } from './useLaunchTerminalSize'

/**
 * The machines that could take a session right now.
 *
 * Both flags matter and mean different things: `enabled` is somebody's
 * decision, `online` is whether the daemon is there. A machine failing either
 * is not offered, because the point of this panel is a button that works —
 * the machines page is where a broken machine gets explained and fixed.
 */
export function launchableMachines(machines) {
  return (machines ?? []).filter((m) => m?.enabled && m?.online)
}

/**
 * Whether a machine has been chosen that can actually run this.
 *
 * Kept apart from the workspace's own eligibility because the fixes are in
 * different places: one is a workspace setting, the other is a machine that is
 * off, and telling somebody to edit a workspace when the answer is "turn a
 * computer on" is how a helpful message wastes an afternoon.
 */
export function machineChoiceEligibility(machineId, machines) {
  const available = launchableMachines(machines)
  if (available.length === 0) {
    return {
      ok: false,
      reason: 'No machine is online to run an agent on.',
      fix: { label: 'Set up a machine', to: '/machines' },
    }
  }
  if (!machineId) return { ok: false, reason: 'Pick a machine to run on.' }
  if (!available.some((m) => m.id === machineId)) {
    // The list is live, so the machine chosen a moment ago can go offline
    // while the panel is open. Saying so is better than a launch that fails
    // for a reason the page already knew.
    return { ok: false, reason: 'That machine is no longer online. Pick another one.' }
  }
  return { ok: true }
}

/**
 * The launcher for one workspace.
 *
 * @param {object} deps `workspace` (a ref) plus, in tests, the API functions
 */
export function useWorkspaceAgentLaunch(deps = {}) {
  const {
    workspace,
    // The fork's parent, when the workspace is a fork: its folder is what a
    // fork's is made from, so it decides whether the launch can work.
    parent,
    fetchMachines = api.fetchMachines,
    launchAgent = api.launchAgent,
    measureTerminalSize = launchTerminalSize,
    fetchAcpAgents,
    fetchAcpModels,
    registryAgents,
    recordTelemetry,
  } = deps

  const machines = ref([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  const launching = ref(false)

  const machineId = ref('')
  const kind = ref(KINDS[0].id)
  const params = useKindParams(kind, () => workspace?.value?.id)

  const { acpAgents, acpModels, claudeModels, pickAcpAgent } = useAcpGatewaySuggestions({
    kind,
    params,
    getMachineId: () => machineId.value,
    getWorkspaceId: () => workspace?.value?.id,
    ...(fetchAcpAgents ? { fetchAcpAgents } : {}),
    ...(fetchAcpModels ? { fetchAcpModels } : {}),
    ...(registryAgents ? { registryAgents } : {}),
    ...(recordTelemetry ? { recordTelemetry } : {}),
  })

  const available = computed(() => launchableMachines(machines.value))
  const selected = computed(() => available.value.find((m) => m.id === machineId.value) ?? null)

  /**
   * Whether to offer this at all.
   *
   * Only once the machines are known, and only with somewhere to run: an
   * offer that resolves to "you have no machines" is worse than the setup
   * guide that was already there, because it looks like a way forward and
   * is not. Gated on `loaded` so the button does not flicker in and out
   * while the list is arriving.
   */
  const offered = computed(
    () =>
      loaded.value &&
      available.value.length > 0 &&
      !!workspace?.value &&
      !workspace.value.agentConnected
  )

  /**
   * Whether there is no machine to run on at all, once the list is known.
   *
   * Not the opposite of `offered`: that one is also false while an agent is
   * connected, and a page that points somebody at setting up a machine should
   * do so only when they have none.
   */
  const noMachine = computed(() => loaded.value && available.value.length === 0)

  /**
   * Every reason this launch would be refused, worst first.
   *
   * The machine leads: nothing runs on a machine that is off, whatever the
   * workspace's settings say.
   */
  const blockers = computed(() =>
    [
      machineChoiceEligibility(machineId.value, machines.value),
      workspaceEligibility(workspace?.value, parent?.value ?? null),
      paramsEligibility(kind.value, params.value),
    ].filter((e) => !e.ok)
  )

  const canLaunch = computed(() => blockers.value.length === 0 && !launching.value)

  /**
   * Load the machines, and preselect one when the choice is obvious.
   *
   * One machine is the common case and picking it saves a step. Several is a
   * real decision and is left to the person, because guessing wrong starts an
   * agent on the wrong computer.
   */
  async function load() {
    loading.value = true
    error.value = ''
    try {
      const data = await fetchMachines()
      machines.value = data?.machines ?? []
      if (!machineId.value) {
        const start = launchStart(workspace?.value?.id, launchableMachines(machines.value))
        machineId.value = start.machineId
        if (start.kind) kind.value = start.kind
      }
      loaded.value = true
    } catch (e) {
      error.value = e?.message || 'Failed to load machines'
    } finally {
      loading.value = false
    }
  }

  /**
   * Ask the machine to start an agent for this workspace.
   *
   * Resolving means the daemon has been asked, not that anything is running —
   * the session reports its own state, and the folder might not even exist on
   * that machine.
   */
  async function launch() {
    if (!canLaunch.value) return null
    launching.value = true
    error.value = ''
    try {
      const extra = launchParamsPayload(kind.value, params.value)

      const { cols, rows } = await measureTerminalSize()
      const created = await launchAgent(workspace.value.id, {
        machineId: machineId.value,
        kind: kind.value,
        cols,
        rows,
        ...extra,
      })
      rememberParams(kind.value, extra)
      rememberLaunchChoice(workspace.value.id, { machineId: machineId.value, kind: kind.value, params: extra })
      return created?.session ?? null
    } catch (e) {
      error.value = e?.message || 'Failed to start the agent'
      return null
    } finally {
      launching.value = false
    }
  }

  return {
    machines,
    available,
    selected,
    loading,
    loaded,
    error,
    launching,
    machineId,
    kind,
    params,
    offered,
    noMachine,
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
