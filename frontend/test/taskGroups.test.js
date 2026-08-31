import { describe, it, expect } from 'vitest'

import { buildTaskGroups, pendingOnHuman } from '../src/composables/useTaskGroups'

const task = (props) => ({ id: Math.random().toString(36).slice(2), messages: [], ...props })
const titles = (groups) => groups.map((g) => g.title)
const titled = (groups, title) => groups.find((g) => g.title === title)

/** Orders cron tasks by a number parked on the schedule string, for legibility. */
const nextRunAt = (schedule) => new Date(Number(schedule))

describe('buildTaskGroups', () => {
  describe('the Scheduled page', () => {
    it('shows the section even when nothing is scheduled', () => {
      // The bug: the section was omitted when empty, so the page rendered an
      // entirely blank column — no heading, no count, no "none found" card,
      // while every other list page showed one.
      const groups = buildTaskGroups([task({ status: 'ongoing' })], 'scheduled', { nextRunAt })

      expect(titles(groups)).toEqual(['Scheduled'])
      expect(titled(groups, 'Scheduled').tasks).toEqual([])
    })

    it('shows the section with nothing at all in the list', () => {
      const groups = buildTaskGroups([], 'scheduled', { nextRunAt })

      expect(titles(groups)).toEqual(['Scheduled'])
    })

    it('orders what is scheduled by which fires next', () => {
      const soon = task({ status: 'cron', cronSchedule: '200' })
      const later = task({ status: 'cron', cronSchedule: '900' })

      const groups = buildTaskGroups([later, soon], 'scheduled', { nextRunAt })

      expect(titled(groups, 'Scheduled').tasks).toEqual([soon, later])
    })

    it('keeps the given order when it cannot tell when they run', () => {
      const a = task({ status: 'cron', cronSchedule: '900' })
      const b = task({ status: 'cron', cronSchedule: '200' })

      expect(titled(buildTaskGroups([a, b], 'scheduled'), 'Scheduled').tasks).toEqual([a, b])
    })
  })

  describe('the Active page', () => {
    it('keeps Scheduled out of the way when nothing is scheduled', () => {
      // Here it is one section among several, so an empty one is noise. This
      // is the behaviour the Scheduled page was wrongly inheriting.
      const groups = buildTaskGroups([task({ status: 'ongoing' })], 'active', { nextRunAt })

      expect(titles(groups)).toEqual(['Ongoing', 'Not Started'])
    })

    it('brings Scheduled in once something is scheduled', () => {
      const groups = buildTaskGroups([task({ status: 'cron', cronSchedule: '1' })], 'active', {
        nextRunAt,
      })

      expect(titles(groups)).toEqual(['Ongoing', 'Not Started', 'Scheduled'])
    })

    it('shows Ongoing and Not Started even when both are empty', () => {
      expect(titles(buildTaskGroups([], 'active', { nextRunAt }))).toEqual([
        'Ongoing',
        'Not Started',
      ])
    })

    it('adds Blocked only when something is blocked', () => {
      expect(titles(buildTaskGroups([], 'active'))).not.toContain('Blocked')
      expect(titles(buildTaskGroups([task({ status: 'blocked' })], 'active'))).toContain('Blocked')
    })
  })

  describe('the other pages', () => {
    it('shows Completed empty, and Rejected only when there is one', () => {
      expect(titles(buildTaskGroups([], 'completed'))).toEqual(['Completed'])
      expect(titles(buildTaskGroups([task({ status: 'rejected' })], 'completed'))).toEqual([
        'Completed',
        'Rejected',
      ])
    })

    it('shows Action Required empty', () => {
      expect(titles(buildTaskGroups([], 'pending'))).toEqual(['Action Required'])
    })

    it('shows Ongoing on its own, without Not Started', () => {
      expect(titles(buildTaskGroups([], 'ongoing'))).toEqual(['Ongoing'])
    })

    it('shows Not Started on its own, without Ongoing', () => {
      expect(titles(buildTaskGroups([], 'notstarted'))).toEqual(['Not Started'])
    })

    it('treats a missing filter as the Active page', () => {
      expect(titles(buildTaskGroups([], undefined))).toEqual(['Ongoing', 'Not Started'])
      expect(titles(buildTaskGroups([], ''))).toEqual(['Ongoing', 'Not Started'])
    })

    it('survives being handed no task list at all', () => {
      expect(titles(buildTaskGroups(null, 'scheduled'))).toEqual(['Scheduled'])
      expect(titles(buildTaskGroups(undefined, 'completed'))).toEqual(['Completed'])
    })
  })
})

describe('pendingOnHuman', () => {
  it('counts a not-started task assigned to the person', () => {
    const mine = task({ status: 'notstarted', assignee: 'human' })

    expect(pendingOnHuman([mine, task({ status: 'notstarted', assignee: 'agent' })])).toEqual([mine])
  })

  it('counts a task waiting on a permission decision', () => {
    const waiting = task({
      status: 'ongoing',
      messages: [{ metadata: { type: 'permission_request', status: 'pending' } }],
    })
    const answered = task({
      status: 'ongoing',
      messages: [{ metadata: { type: 'permission_request', status: 'approved' } }],
    })

    expect(pendingOnHuman([waiting, answered])).toEqual([waiting])
  })

  it('ignores tasks that are already finished', () => {
    // A completed task with an unanswered permission request is history, not
    // something still being asked of anyone.
    const done = task({
      status: 'completed',
      messages: [{ metadata: { type: 'permission_request', status: 'pending' } }],
    })

    expect(pendingOnHuman([done, task({ status: 'rejected', assignee: 'human' })])).toEqual([])
  })

  it('copes with a task that has no messages', () => {
    expect(pendingOnHuman([task({ status: 'ongoing', messages: undefined })])).toEqual([])
  })
})
