// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed, ref, watch } from 'vue'
import { useCron } from './useCron'

/**
 * What a new task carries besides its words: who it is for, whether the agent
 * may run every command, whether it starts on a clean context, when it runs,
 * what it fires on completion, and its attachments. The New Task form and the
 * Pano's chat box both draw these as `TaskOptionsTools` and send them to
 * `createTask`, so a control means the same thing in both.
 */
export function useTaskOptions() {
  const { getNextRunLabel, daysOptions } = useCron()

  const assignee = ref('agent')
  const allowAllCommands = ref(false)
  const clearContext = ref(false)
  const cronSchedule = ref('')
  const attachments = ref([])

  // What fires when the task completes: a single event, or a whole workflow.
  // Mutually exclusive by construction — picking one clears the other.
  const events = ref([])
  const workflows = ref([])
  const selectedEventId = ref('')
  const selectedWorkflowId = ref('')
  const completionKind = ref('event')

  const completionTriggerLabel = computed(() => {
    if (selectedWorkflowId.value) {
      const wf = workflows.value.find((w) => w.id === selectedWorkflowId.value)
      return `Runs workflow ${wf?.name ?? ''} on completion`
    }
    if (selectedEventId.value) return 'Event Emitted on Completion'
    return 'Emit Event or Run Workflow on Completion'
  })

  // Scheduling state
  const scheduleType = ref('none')
  const oneTimeDate = ref('')
  const repeatPreset = ref('daily')
  const repeatTime = ref('09:00')
  const selectedDays = ref([1, 2, 3, 4, 5]) // Mon-Fri

  const nextRunPreview = computed(() => {
    if (scheduleType.value === 'none' || !cronSchedule.value) return ''
    return getNextRunLabel(cronSchedule.value)
  })

  function toggleDay(day) {
    const idx = selectedDays.value.indexOf(day)
    if (idx === -1) selectedDays.value.push(day)
    else if (selectedDays.value.length > 1) selectedDays.value.splice(idx, 1)
  }

  /** The workspace's own defaults, for a task that has not been touched yet. */
  function applyWorkspaceDefaults(workspace) {
    allowAllCommands.value = workspace?.allowAllCommands || false
    clearContext.value = workspace?.clearContextDefault || false
  }

  function parseCronToUI(cron) {
    const parts = cron.split(' ')
    if (parts.length === 5 && parts[2] !== '*' && parts[3] !== '*') {
      scheduleType.value = 'onetime'
      const [min, hour, dom, month] = parts
      const currentYear = new Date().getFullYear()
      const utcDate = new Date(Date.UTC(currentYear, month - 1, dom, hour, min))
      const year = utcDate.getFullYear()
      const mon = String(utcDate.getMonth() + 1).padStart(2, '0')
      const day = String(utcDate.getDate()).padStart(2, '0')
      const hh = String(utcDate.getHours()).padStart(2, '0')
      const mm = String(utcDate.getMinutes()).padStart(2, '0')
      oneTimeDate.value = `${year}-${mon}-${day}T${hh}:${mm}`
    } else {
      scheduleType.value = 'repeated'
      if (cron === '*/15 * * * *') {
        repeatPreset.value = '15min'
      } else if (cron === '*/30 * * * *') {
        repeatPreset.value = '30min'
      } else if (cron === '0 * * * *') {
        repeatPreset.value = 'hourly'
      } else if (cron === '0 */2 * * *') {
        repeatPreset.value = '2hour'
      } else {
        const [min, hour, dom, month, dow] = parts
        const firstHour = Number(hour.split(',')[0])
        const now = new Date()
        const d = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate(), firstHour, min))
        repeatTime.value = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`

        if (dom === '*' && month === '*' && dow === '*') {
          const hoursArr = hour.split(',').map(Number)
          if (hoursArr.length === 2 && Math.abs(hoursArr[1] - hoursArr[0]) === 12) {
            repeatPreset.value = '12hour'
          } else {
            repeatPreset.value = 'daily'
          }
        } else if (dow !== '*' && dom === '*' && month === '*') {
          const utcDays = dow.split(',').map(Number)
          const localDays = utcDays.map((ud) => {
            const base = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()))
            const currentUTCDay = base.getUTCDay()
            const offset = ud - currentUTCDay
            const temp = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate() + offset, firstHour, min))
            return temp.getDay()
          })
          selectedDays.value = localDays
          repeatPreset.value = (localDays.length === 1 && localDays[0] === 0) ? 'weekly' : 'custom'
        } else {
          repeatPreset.value = 'custom'
        }
      }
    }
  }

  watch([scheduleType, oneTimeDate, repeatPreset, repeatTime, selectedDays], () => {
    if (scheduleType.value === 'none') { cronSchedule.value = ''; return }

    if (scheduleType.value === 'onetime') {
      if (!oneTimeDate.value) { cronSchedule.value = ''; return }
      const d = new Date(oneTimeDate.value)
      cronSchedule.value = `${d.getUTCMinutes()} ${d.getUTCHours()} ${d.getUTCDate()} ${d.getUTCMonth() + 1} *`
      return
    }

    const [localHours, localMinutes] = repeatTime.value.split(':').map(Number)
    const d = new Date()
    d.setHours(localHours, localMinutes, 0, 0)
    const minutes = d.getUTCMinutes()
    const hours = d.getUTCHours()

    if (repeatPreset.value === '15min') {
      cronSchedule.value = '*/15 * * * *'
    } else if (repeatPreset.value === '30min') {
      cronSchedule.value = '*/30 * * * *'
    } else if (repeatPreset.value === 'hourly') {
      cronSchedule.value = '0 * * * *'
    } else if (repeatPreset.value === '2hour') {
      cronSchedule.value = '0 */2 * * *'
    } else if (repeatPreset.value === '12hour') {
      const h1 = d.getUTCHours()
      const tempD = new Date(d)
      tempD.setHours(tempD.getHours() + 12)
      const h2 = tempD.getUTCHours()
      const hoursStr = [h1, h2].sort((a, b) => a - b).join(',')
      cronSchedule.value = `${minutes} ${hoursStr} * * *`
    } else if (repeatPreset.value === 'daily') {
      cronSchedule.value = `${minutes} ${hours} * * *`
    } else if (repeatPreset.value === 'weekly') {
      cronSchedule.value = `${minutes} ${hours} * * ${d.getUTCDay()}`
    } else if (repeatPreset.value === 'monthly') {
      const dd = new Date()
      dd.setHours(localHours, localMinutes, 0, 0)
      dd.setDate(1)
      cronSchedule.value = `${minutes} ${hours} ${dd.getUTCDate()} * *`
    } else {
      // Custom: the chosen weekdays.
      const utcDays = new Set()
      selectedDays.value.forEach((day) => {
        const dd = new Date()
        dd.setHours(localHours, localMinutes, 0, 0)
        const currentDay = dd.getDay()
        dd.setDate(dd.getDate() + (day - currentDay))
        utcDays.add(dd.getUTCDay())
      })
      const days = [...utcDays].sort().join(',')
      cronSchedule.value = `${minutes} ${hours} * * ${days}`
    }
  }, { deep: true })

  function processFiles(files) {
    for (let i = 0; i < files.length; i++) {
      const fn = files[i]
      const reader = new FileReader()
      reader.onload = (e) => {
        attachments.value.push({
          filename: fn.name,
          mimeType: fn.type || 'application/octet-stream',
          data: e.target.result.split(',')[1],
        })
      }
      reader.readAsDataURL(fn)
    }
  }

  /** Back to a task nobody has touched, as after one is sent. */
  function reset(workspace) {
    assignee.value = 'agent'
    attachments.value = []
    scheduleType.value = 'none'
    selectedEventId.value = ''
    selectedWorkflowId.value = ''
    completionKind.value = 'event'
    applyWorkspaceDefaults(workspace)
  }

  return {
    assignee, allowAllCommands, clearContext, cronSchedule, attachments,
    events, workflows, selectedEventId, selectedWorkflowId, completionKind, completionTriggerLabel,
    scheduleType, oneTimeDate, repeatPreset, repeatTime, selectedDays, daysOptions, nextRunPreview,
    toggleDay, parseCronToUI, applyWorkspaceDefaults, processFiles, reset,
  }
}
