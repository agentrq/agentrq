// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The machines page's state, and the rules it needs.
 *
 * All of it lives here rather than in the view: which machine an event
 * belongs to, what an event may and may not overwrite, and what happens when
 * one arrives for a machine the page has never heard of are all decisions,
 * and decisions in a template are decisions nobody tests.
 */

import { ref, computed } from 'vue'
import * as api from '../api'

/**
 * Fold a live update into a machine.
 *
 * Merged rather than replaced, and this is the important part: the events a
 * daemon produces are partial by design — a heartbeat carries metrics and
 * nothing else, a session change carries a status. Replacing would blank the
 * name, the OS and everything else the list had, on every heartbeat.
 */
export function mergeMachine(current, incoming) {
  if (!incoming?.id) return current
  if (!current) return null
  if (current.id !== incoming.id) return current
  const merged = { ...current }
  for (const [key, value] of Object.entries(incoming)) {
    if (value !== undefined && value !== null) merged[key] = value
  }
  return merged
}

/**
 * Apply an event to a list of machines.
 *
 * A machine the list does not have is ignored rather than added: the event
 * carries a fragment, and a row built from a fragment would be a machine with
 * no name. The next load has the whole thing.
 */
export function applyMachineEvent(machines, event) {
  if (event?.type !== 'machine.updated') return machines
  const payload = event.payload
  if (!payload?.id) return machines
  return machines.map((m) => (m.id === payload.id ? mergeMachine(m, payload) : m))
}

/**
 * Apply a session event to a list of machines.
 *
 * Only the count moves. The machines page shows how many agents are on each
 * box, and a session starting or ending is exactly when that number changes —
 * but the event does not carry the new total, so it is stepped rather than
 * assigned, and never below zero.
 */
export function applySessionEventToMachines(machines, event, wasLive) {
  if (event?.type !== 'session.updated') return machines
  const payload = event.payload
  if (!payload?.machineId) return machines

  const nowLive = payload.status === 'starting' || payload.status === 'running'
  const delta = (nowLive ? 1 : 0) - (wasLive ? 1 : 0)
  if (delta === 0) return machines

  return machines.map((m) =>
    m.id === payload.machineId ? { ...m, sessions: Math.max(0, (m.sessions ?? 0) + delta) } : m
  )
}

/**
 * The machines list.
 *
 * @param {object} [deps] injected for tests; defaults to the real API
 */
export function useMachines(deps = {}) {
  const {
    fetchMachines = api.fetchMachines,
    createEnrolmentCode = api.createEnrolmentCode,
    serverOrigin = api.serverOrigin,
  } = deps

  const machines = ref([])
  const loading = ref(true)
  const error = ref('')
  const enrolmentCode = ref(null)
  const origin = ref('')

  // Which sessions this page believes are live, so a session event can step
  // the right machine's count. Without it, a "running" report for a session
  // that was already running would count the same agent twice.
  const liveSessions = new Set()

  /**
   * The command to run on the machine being enrolled.
   *
   * The server address comes from the shell rather than from the page's own
   * origin, because on the desktop build that origin is `app://` — an address
   * no daemon could ever reach. Printing it would give somebody a command that
   * cannot work and no clue why.
   */
  const enrolCommand = computed(() => {
    if (!enrolmentCode.value?.code) return ''
    const server = origin.value || ''
    return `agentrqd enroll --server ${server} --code ${enrolmentCode.value.code}`
  })

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const data = await fetchMachines()
      machines.value = data?.machines ?? []
    } catch (e) {
      error.value = e?.message || 'Failed to load machines'
    } finally {
      loading.value = false
    }
  }

  async function requestCode() {
    error.value = ''
    try {
      // The address first: a code shown beside a command with no server in it
      // is a command somebody will copy and then have to debug.
      origin.value = await serverOrigin()
      enrolmentCode.value = await createEnrolmentCode()
      return enrolmentCode.value
    } catch (e) {
      error.value = e?.message || 'Failed to create an enrolment code'
      return null
    }
  }

  function dismissCode() {
    enrolmentCode.value = null
  }

  function handleEvent(event) {
    if (event?.type === 'machine.updated') {
      machines.value = applyMachineEvent(machines.value, event)
      return
    }
    if (event?.type === 'session.updated') {
      // The id first, and nothing without it. The count is stepped rather than
      // assigned, so an event this page cannot remember having seen would step
      // a number it can never step back — and the machine's agent count would
      // drift upwards for the rest of the session.
      const id = event.payload?.id
      if (!id) return

      const wasLive = liveSessions.has(id)
      machines.value = applySessionEventToMachines(machines.value, event, wasLive)

      if (event.payload?.status === 'starting' || event.payload?.status === 'running') {
        liveSessions.add(id)
      } else {
        liveSessions.delete(id)
      }
    }
  }

  return {
    machines,
    loading,
    error,
    enrolmentCode,
    enrolCommand,
    load,
    requestCode,
    dismissCode,
    handleEvent,
  }
}
