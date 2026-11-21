// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * One machine, its sessions, and everything that can be done to it.
 *
 * The destructive parts are the reason this is a composable rather than a
 * view: what an update destroys, and whether a kill has actually happened, are
 * both decisions that must be right and neither belongs in a template.
 */

import { ref, computed } from 'vue'
import * as api from '../api'
import { isSessionLive } from './useMachineFormat'

/**
 * What an update to this machine will destroy, in words.
 *
 * A bare "Update?" is not consent. Updating a daemon restarts it, which ends
 * every session it is supervising — and the person pressing the button is
 * usually not the person whose agent is mid-task. The count is named, and so
 * is what happens next, because "3 sessions" alone does not say whether they
 * come back.
 */
export function updateConsequence(sessionCount) {
  const n = Number.isFinite(sessionCount) ? Math.max(0, sessionCount) : 0
  if (n === 0) return 'Nothing is running on it.'
  if (n === 1) return 'Stops 1 running session and starts it again. Unsaved work is lost.'
  return `Stops ${n} running sessions and starts them again. Unsaved work is lost.`
}

/** The first agentrqd that can be restarted or updated from here; wire.MinRemoteControlVersion. */
export const REMOTE_CONTROL_MIN_VERSION = '0.9.3'

/**
 * Whether a daemon's version is `min` or later, as wire.VersionAtLeast
 * decides it: semver, a pre-release below its release, and anything that is
 * not a release ("dev", empty) below everything.
 */
export function versionAtLeast(version, min) {
  const parse = (s) => {
    const m = /^(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.]+)?$/.exec(String(s ?? ''))
    return m ? { core: [+m[1], +m[2], +m[3]], pre: Boolean(m[4]) } : null
  }
  const v = parse(version)
  const floor = parse(min)
  if (!v || !floor) return false
  for (let i = 0; i < 3; i++) {
    if (v.core[i] !== floor.core[i]) return v.core[i] > floor.core[i]
  }
  return !v.pre
}

/**
 * What deleting this machine destroys.
 *
 * Deleting is not disabling: the token stops working, so the machine has to be
 * enrolled again by somebody standing at it. That is the part people do not
 * expect, so it is the part that is said.
 */
export function deleteConsequence(machine, sessionCount) {
  const n = Number.isFinite(sessionCount) ? Math.max(0, sessionCount) : 0
  const name = machine?.name || 'this machine'
  const running =
    n > 0 ? ` ${n === 1 ? '1 session is' : `${n} sessions are`} still running on it.` : ''
  return `${name} will stop being able to connect, and enrolling it again means running the enrolment command on the machine itself.${running}`
}

/**
 * One machine and its sessions.
 *
 * @param {object} deps `machineId` plus, in tests, the API functions
 */
export function useMachineDetail(deps = {}) {
  const {
    machineId,
    getMachine = api.getMachine,
    fetchMachineSessions = api.fetchMachineSessions,
    updateMachine = api.updateMachine,
    deleteMachine = api.deleteMachine,
    killSession = api.killSession,
    restartDaemon = api.restartDaemon,
  } = deps

  const machine = ref(null)
  const sessions = ref([])
  const loading = ref(true)
  const error = ref('')
  const busy = ref(false)

  const liveSessions = computed(() => sessions.value.filter((s) => isSessionLive(s.status)))

  // The server refuses an older daemon too; this is so the page says so first.
  const canControl = computed(() => versionAtLeast(machine.value?.version, REMOTE_CONTROL_MIN_VERSION))

  // `quiet` re-reads without the loading line, for a refresh the person did not ask for.
  async function load({ quiet = false } = {}) {
    if (!quiet) loading.value = true
    error.value = ''
    try {
      const [m, s] = await Promise.all([getMachine(machineId), fetchMachineSessions(machineId)])
      machine.value = m?.machine ?? null
      sessions.value = s?.sessions ?? []
    } catch (e) {
      error.value = e?.message || 'Failed to load this machine'
    } finally {
      loading.value = false
    }
  }

  async function rename(name) {
    const trimmed = (name ?? '').trim()
    if (!trimmed || trimmed === machine.value?.name) return false
    return change({ name: trimmed }, 'Failed to rename this machine')
  }

  /**
   * Turn the machine on or off.
   *
   * Disabling is the kill switch: the server closes the socket rather than
   * waiting for the daemon to notice, so this takes effect whether or not the
   * machine cooperates.
   */
  async function setEnabled(enabled) {
    return change({ enabled }, enabled ? 'Failed to enable this machine' : 'Failed to disable this machine')
  }

  /**
   * Cap the agents this account may run here: a number, 0 for no cap, or
   * null for the server's default.
   */
  async function setMaxSessions(maxSessions) {
    return change({ maxSessions }, "Failed to set this machine's agent limit")
  }

  async function change(patch, failure) {
    busy.value = true
    error.value = ''
    try {
      const updated = await updateMachine(machineId, patch)
      // Merged rather than replaced: an update response carries the machine
      // without its session count or metrics, and taking it whole would blank
      // both every time somebody renamed a box.
      machine.value = { ...machine.value, ...(updated?.machine ?? {}) }
      return true
    } catch (e) {
      error.value = e?.message || failure
      return false
    } finally {
      busy.value = false
    }
  }

  async function remove() {
    busy.value = true
    error.value = ''
    try {
      await deleteMachine(machineId)
      return true
    } catch (e) {
      error.value = e?.message || 'Failed to delete this machine'
      return false
    } finally {
      busy.value = false
    }
  }

  /**
   * Ask the daemon to end a session.
   *
   * The row is not marked dead here. The daemon reports what actually
   * happened, and a list that said "killed" for a process still running would
   * be a kill switch that lies about having worked.
   */
  async function stop(sessionId) {
    busy.value = true
    error.value = ''
    try {
      await killSession(sessionId)
      return true
    } catch (e) {
      error.value = e?.message || 'Failed to stop the session'
      return false
    } finally {
      busy.value = false
    }
  }

  // One command for both: an offered release is installed on the way, since
  // installing it restarts the daemon anyway.
  const updating = computed(() => Boolean(machine.value?.availableVersion))

  /**
   * Restart the daemon, updating it to the offered release when there is one.
   * Every session is stopped and started again. Answered once the daemon has
   * been asked; its next hello says it is back.
   */
  async function restart() {
    busy.value = true
    error.value = ''
    try {
      await restartDaemon(machineId, machine.value?.availableVersion || '')
      return true
    } catch (e) {
      error.value = e?.message || (updating.value ? 'Failed to update this machine' : 'Failed to restart this machine')
      return false
    } finally {
      busy.value = false
    }
  }

  /** Fold a live update in. */
  function handleEvent(event) {
    if (event?.type === 'machine.updated') {
      const payload = event.payload
      if (!payload?.id || payload.id !== machine.value?.id) return
      machine.value = { ...machine.value, ...payload }
      return
    }
    if (event?.type !== 'session.updated') return

    const payload = event.payload
    if (!payload?.id) return
    const index = sessions.value.findIndex((s) => s.id === payload.id)

    // A session that has ended leaves the list, because the row behind it has
    // gone too: this page answers "what is running on this machine", and a
    // finished agent is not an answer to that. Reloading would show the same
    // thing; this just shows it immediately.
    if (isSessionLive(payload.status) === false && payload.status) {
      if (index !== -1) {
        sessions.value = sessions.value.filter((s) => s.id !== payload.id)
      }
      return
    }

    if (index === -1) {
      // A session this page has never seen. Only added when it belongs here,
      // and only ever as what the event carries: a row built from a fragment
      // is a session with no kind and no start time, but a session that
      // appeared while somebody was watching should still appear.
      if (payload.machineId === machine.value?.id) {
        sessions.value = [payload, ...sessions.value]
      }
      return
    }
    const next = sessions.value.slice()
    next[index] = { ...next[index], ...payload }
    sessions.value = next
  }

  return {
    machine,
    sessions,
    liveSessions,
    canControl,
    loading,
    error,
    busy,
    load,
    rename,
    setEnabled,
    setMaxSessions,
    remove,
    stop,
    updating,
    restart,
    handleEvent,
  }
}
