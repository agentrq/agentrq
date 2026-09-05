import { describe, it, expect } from 'vitest'

import {
  statusTone,
  timeAgo,
  toneDotClass,
  toneEdgeClass,
  toneChipClass,
  toneLabel,
  toneSurfaceClass,
  toneTitleClass,
} from '../src/composables/useTaskCard'

const task = (over = {}) => ({ status: 'ongoing', assignee: 'agent', ...over })

describe('statusTone', () => {
  it('answers for every status the product has', () => {
    expect(statusTone('ongoing')).toBe('active')
    expect(statusTone('notstarted')).toBe('idle')
    expect(statusTone('completed')).toBe('done')
    expect(statusTone('rejected')).toBe('rejected')
    expect(statusTone('blocked')).toBe('blocked')
    expect(statusTone('cron')).toBe('scheduled')
  })

  it('answers by status alone, whoever the task is on', () => {
    // An earlier draft promoted "waiting on you" above the status and painted
    // those cards yellow, which put two colours in one column and made the
    // board disagree with its own headings.
    expect(statusTone(task({ status: 'notstarted', assignee: 'human' }))).toBe('idle')
    expect(statusTone(task({ status: 'notstarted', assignee: 'agent' }))).toBe('idle')
  })

  it('takes a bare status as well as a whole task', () => {
    expect(statusTone('notstarted')).toBe('idle')
  })

  it('has an answer for a status it has never heard of', () => {
    expect(statusTone('teleported')).toBe('unknown')
    expect(statusTone(null)).toBe('unknown')
    expect(statusTone({})).toBe('unknown')
  })
})

describe('the visual helpers', () => {
  it('give every tone a dot, an edge and a word', () => {
    // A tone with no class would render an unstyled card rather than fail
    // loudly, so this is what would catch a tone added later without colours.
    const statuses = ['ongoing', 'notstarted', 'completed', 'rejected', 'blocked', 'cron', 'nonsense']

    for (const status of statuses) {
      expect(toneDotClass(status)).toBeTruthy()
      expect(toneEdgeClass(status)).toBeTruthy()
      expect(toneLabel(status)).toBeTruthy()
      expect(toneSurfaceClass(status)).toBeTruthy()
      expect(toneChipClass(status)).toBeTruthy()
      expect(toneTitleClass(status)).toBeTruthy()
    }
  })

  it('pulses a running card but never a column heading', () => {
    // A card says "this one is running"; a heading only names itself, and a
    // heading that throbs at you is noise.
    expect(toneDotClass('ongoing', { pulse: true })).toContain('animate-pulse')
    expect(toneDotClass('ongoing')).not.toContain('animate-pulse')
    expect(toneDotClass('blocked', { pulse: true })).not.toContain('animate-pulse')
  })

  it('uses the palette the board was asked to match', () => {
    // Sampled from the design: grey not started, blue in progress, amber
    // blocked, green done. A board-only palette, deliberately different from
    // the dots the feed and the detail view use.
    expect(toneDotClass('notstarted')).toContain('gray-400')
    expect(toneDotClass('ongoing')).toContain('blue-500')
    expect(toneDotClass('blocked')).toContain('amber-500')
    expect(toneDotClass('completed')).toContain('green-500')
    expect(toneDotClass('rejected')).toContain('red-500')
    expect(toneDotClass('cron')).toContain('cyan')
    expect(toneDotClass('nonsense')).toContain('gray-300')
  })

  it('carries the sampled values into dark mode exactly', () => {
    // The light steps go a shade deeper because a colour picked to sit on
    // near-black does not carry on white; dark keeps the sampled pixels.
    expect(toneEdgeClass('ongoing')).toContain('#60aaf3')
    expect(toneEdgeClass('blocked')).toContain('#eeb254')
    expect(toneEdgeClass('completed')).toContain('#67d282')
    expect(toneEdgeClass('notstarted')).toContain('#9b9fa7')
  })

  it('gives the edge a value in both themes rather than deriving one', () => {
    // A colour that reads as "stopped" on white can read as decorative on
    // black, so neither theme is left to an opacity.
    for (const status of ['ongoing', 'notstarted', 'completed', 'blocked', 'cron']) {
      expect(toneEdgeClass(status)).toMatch(/border-l-/)
      expect(toneEdgeClass(status)).toMatch(/dark:border-l-/)
    }
  })

  it('names each state for the hover and for assistive technology', () => {
    expect(toneLabel('notstarted')).toBe('Not started')
    expect(toneLabel('blocked')).toBe('Blocked')
    expect(toneLabel('completed')).toBe('Done')
  })

  it('gives each column its own edge colour', () => {
    expect(toneEdgeClass('notstarted')).toContain('gray')
    expect(toneEdgeClass('ongoing')).toContain('blue')
    expect(toneEdgeClass('blocked')).toContain('amber')
    expect(toneEdgeClass('completed')).toContain('green')
  })

  it('recesses finished work in the text, not in the colour', () => {
    // Completed keeps the green the app already means by it; it is the title
    // that goes quiet, so a long Done column does not shout while still
    // reading as done rather than as some grey state of its own.
    expect(toneTitleClass('completed')).toContain('text-gray-400')
    expect(toneTitleClass('rejected')).toContain('text-gray-400')
    expect(toneTitleClass('ongoing')).toContain('text-gray-700')
    expect(toneEdgeClass('completed')).toContain('green')
  })

  it('tints the assignee chip to the status rather than leaving it neutral', () => {
    // Edge, ground and chip carry the tone three times, which is what lets the
    // state survive being glanced at.
    expect(toneChipClass('blocked')).toContain('amber')
    expect(toneChipClass('ongoing')).toContain('blue')
    expect(toneChipClass('cron')).toContain('cyan')
    expect(toneChipClass('notstarted')).toContain('gray')
  })
})

describe('timeAgo', () => {
  const NOW = new Date('2026-09-05T12:00:00Z').getTime()
  const ago = (ms) => timeAgo(new Date(NOW - ms).toISOString(), NOW)

  it('reads at every scale a board needs', () => {
    expect(ago(30 * 1000)).toBe('just now')
    expect(ago(5 * 60 * 1000)).toBe('5m')
    expect(ago(3 * 60 * 60 * 1000)).toBe('3h')
    expect(ago(4 * 24 * 60 * 60 * 1000)).toBe('4d')
    expect(ago(800 * 24 * 60 * 60 * 1000)).toBe('2y')
  })

  it('changes unit exactly at the boundary', () => {
    expect(ago(59 * 1000)).toBe('just now')
    expect(ago(60 * 1000)).toBe('1m')
    expect(ago(59 * 60 * 1000)).toBe('59m')
    expect(ago(60 * 60 * 1000)).toBe('1h')
    expect(ago(23 * 60 * 60 * 1000)).toBe('23h')
    expect(ago(24 * 60 * 60 * 1000)).toBe('1d')
  })

  it('does not show a negative age when the clocks disagree', () => {
    expect(timeAgo(new Date(NOW + 5000).toISOString(), NOW)).toBe('just now')
  })

  it('says nothing rather than something wrong for a date it cannot read', () => {
    expect(timeAgo('not a date', NOW)).toBe('')
    expect(timeAgo(null, NOW)).toBe('')
    expect(timeAgo(undefined, NOW)).toBe('')
  })
})
