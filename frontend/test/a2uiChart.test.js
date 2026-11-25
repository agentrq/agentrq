// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A card's Chart, from our catalog: checked as the server checks it, drawn
 * with the analytics pages' charts in their colours, or the agent's.
 */

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

import A2uiSurface from '../src/components/a2ui/A2uiSurface.vue'
import BarChart from '../src/components/BarChart.vue'
import ChartSVG from '../src/components/ChartSVG.vue'
import HeatmapChart from '../src/components/HeatmapChart.vue'
import { chartColor, chartPoints, chartSummary, heatmapProps } from '../src/a2ui/chart.js'
import { AGENTRQ_CATALOG_ID, BASIC_CATALOG_ID, MAX_CHART_POINTS, validateMessages } from '../src/a2ui/validate.js'
import { useThemeStore } from '../src/stores/themeStore'

const v = (body) => ({ version: '1.0', ...body })
const messages = (components, { catalogId = AGENTRQ_CATALOG_ID, dataModel } = {}) => [
  v({ createSurface: { surfaceId: 's1', catalogId } }),
  v({ updateComponents: { surfaceId: 's1', components: [{ id: 'root', component: 'Column', children: components.map((c) => c.id) }, ...components] } }),
  ...(dataModel ? [v({ updateDataModel: { surfaceId: 's1', path: '/', value: dataModel } })] : []),
]
const chart = (extra = {}) => ({ id: 'c', component: 'Chart', data: [{ label: 'Mon', value: 2 }, { label: 'Tue', value: 5 }], ...extra })
const days = (n, from = '2026-01-01') => Array.from({ length: n }, (_, i) => {
  const d = new Date(`${from}T00:00:00`)
  d.setDate(d.getDate() + i)
  const pad = (x) => String(x).padStart(2, '0')
  return { label: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`, value: i }
})

const apps = []
function mount(component, props) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(component, props) })
  apps.push(app)
  app.mount(el)
  return el
}
const card = (components, opts) => mount(A2uiSurface, reactive({
  metadata: { type: 'a2ui', version: '1.0', surfaceId: 's1', messages: messages(components, opts) }, text: 'the fallback',
}))
const q = (el, test) => el.querySelector(`[data-test="${test}"]`)

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  while (apps.length) apps.pop().unmount()
  document.body.innerHTML = ''
})

describe('a Chart, checked', () => {
  const ok = (components, opts) => validateMessages(messages(components, opts))
  it('is drawn only on a surface in our catalog', () => {
    expect(ok([chart()]).ok).toBe(true)
    expect(ok([chart()], { catalogId: BASIC_CATALOG_ID }).problems[0]).toMatch(/component "Chart" is not allowed here/)
    expect(ok([chart()], { catalogId: 'acme' }).problems[0]).toMatch(/catalogId must be the basic catalog or ours/)
  })

  it('takes a variant, a label, a colour of ours or a hex, and a list or a binding', () => {
    expect(ok([chart({ variant: 'bar', label: { '@path': '/l' }, color: '#AEC477', weight: 1 })]).ok).toBe(true)
    expect(ok([chart({ variant: 'heatmap', color: 'messages', data: { '@path': '/points' } })]).ok).toBe(true)
    expect(ok([chart({ color: 'tasks', data: [] })]).ok).toBe(true)
    for (const bad of [
      { variant: 'pie' }, { color: 'red' }, { color: 3 }, { data: [{ label: 'a', value: -1 }] }, { data: [{ label: '', value: 1 }] },
      { data: [{ label: 'x'.repeat(65), value: 1 }] }, { data: [{ label: 'a' }] }, { data: [{ label: 'a', value: 1, extra: 1 }] },
      { data: Array.from({ length: MAX_CHART_POINTS + 1 }, () => ({ label: 'a', value: 1 })) }, { data: 'points' },
    ]) {
      expect(ok([chart(bad)]).ok, JSON.stringify(bad).slice(0, 60)).toBe(false)
    }
    expect(ok([{ id: 'c', component: 'Chart' }]).problems[0]).toMatch(/data is required/)
  })

  it("names the surface's catalog, or the basic one, in @catalogId and a call", () => {
    expect(ok([chart({ '@catalogId': AGENTRQ_CATALOG_ID }), { id: 't', component: 'Text', text: 'x', '@catalogId': BASIC_CATALOG_ID }]).ok).toBe(true)
    const t = { id: 't', component: 'Text', text: 'x', '@catalogId': AGENTRQ_CATALOG_ID }
    expect(ok([t], { catalogId: BASIC_CATALOG_ID }).problems[0]).toMatch(/@catalogId must be the surface's catalog/)
    expect(ok([{ id: 't', component: 'Text', text: { '@call': 'formatString', args: { value: 'x' }, catalogId: AGENTRQ_CATALOG_ID } }]).ok).toBe(true)
  })
})

describe('the chart helpers', () => {
  it('keeps the points that can be drawn, at most as many as the server takes', () => {
    expect(chartPoints(undefined)).toEqual([])
    expect(chartPoints([{ label: 'a', value: 1 }, { label: 'b', value: -1 }, { label: '', value: 1 }, { label: 'c', value: '2' }, null, { label: 'd', value: Infinity }, { label: 'e', value: 0 }]))
      .toEqual([{ label: 'a', value: 1 }, { label: 'e', value: 0 }])
    expect(chartPoints(Array.from({ length: 800 }, () => ({ label: 'a', value: 1 })))).toHaveLength(MAX_CHART_POINTS)
  })

  it('colours a chart in the analytics tones, or the hex given', () => {
    expect(chartColor(undefined, 'line', false)).toBe('#27272a')
    expect(chartColor('tasks', 'bar', true)).toBe('#aec477')
    expect(chartColor('messages', 'line', true)).toBe('#a8a3d9')
    expect(chartColor('messages', 'heatmap', true)).toBe('violet')
    expect(chartColor(undefined, 'heatmap', true)).toBe('yellow')
    expect(chartColor('tasks', 'heatmap', false)).toBe('gray')
    expect(chartColor('#123abc', 'heatmap', true)).toBe('#123abc')
  })

  it('reads a heatmap as days or hours by its first label, over the span the grid draws', () => {
    expect(heatmapProps([])).toEqual({ granularity: 'day', data: [], rangeStart: 0, rangeEnd: 0 })
    const byDay = heatmapProps([{ label: '2026-01-03', value: 1 }, { label: '2026-01-01', value: 2 }, { label: '2026-01-02 10:00', value: 3 }, { label: '2026-13-45', value: 4 }])
    expect(byDay.granularity).toBe('day')
    expect(byDay.data).toEqual([{ bucket: '2026-01-03', count: 1 }, { bucket: '2026-01-01', count: 2 }])
    expect(byDay.rangeStart).toBe(new Date('2026-01-01T00:00:00').getTime() / 1000)
    expect(byDay.rangeEnd).toBe(new Date('2026-01-03T00:00:00').getTime() / 1000)

    const byHour = heatmapProps([{ label: '2026-01-01 09:00', value: 1 }, { label: '2026-01-01', value: 2 }])
    expect(byHour.granularity).toBe('hour')
    expect(byHour.data).toEqual([{ bucket: '2026-01-01 09:00', count: 1 }])
    expect(byHour.rangeStart).toBe(byHour.rangeEnd)

    const year = heatmapProps(days(400))
    expect(year.data).toHaveLength(366)
    expect(year.data[0].bucket).toBe(days(400)[34].label)
  })

  it('says the label and the points to a screen reader', () => {
    expect(chartSummary('PRs', [{ label: 'a', value: 1 }, { label: 'b', value: 2 }])).toBe('PRs. a: 1, b: 2')
    expect(chartSummary('', [{ label: 'a', value: 1 }, { label: 'b', value: 2 }], 1)).toBe('a: 1, and 1 more')
    expect(chartSummary('', [])).toBe('')
  })
})

describe('a Chart, drawn', () => {
  it('draws a line chart by default, in the tasks tone, under its label', async () => {
    const el = card([chart({ label: 'Merged PRs' })])
    await nextTick()
    const fig = q(el, 'a2ui-chart')
    expect(fig.dataset.variant).toBe('line')
    expect(fig.className, 'every other date, when narrow').toContain('@max-xs:')
    expect(fig.querySelector('figcaption').textContent).toContain('Merged PRs')
    expect(fig.querySelector('figcaption span').style.backgroundColor).toBe('rgb(39, 39, 42)')
    expect(fig.querySelector('[role="img"]').getAttribute('aria-label')).toBe('Merged PRs. Mon: 2, Tue: 5')
    expect(fig.querySelector('path').getAttribute('stroke')).toBe('#27272a')
    expect(fig.textContent).toContain('Mon')
  })

  it('draws bars in the dark messages tone, from the data model, with no label', async () => {
    useThemeStore().theme = 'dark'
    const el = card([chart({ variant: 'bar', color: 'messages', data: { '@path': '/points' } })], { dataModel: { points: [{ label: 'a', value: 3 }, { label: 'b', value: -1 }] } })
    await nextTick()
    const fig = q(el, 'a2ui-chart')
    expect(fig.querySelector('figcaption')).toBeNull()
    expect(fig.className, "a bar chart's labels, each in its slot").not.toContain('@max-xs:')
    expect(fig.querySelectorAll('[data-test="bar"]')).toHaveLength(1)
    expect(fig.querySelector('[data-test="bar"] div').style.backgroundColor).toBe('rgb(168, 163, 217)')
  })

  it('draws a heatmap of days, or a taller one of hours, in a hex colour', async () => {
    const el = card([chart({ variant: 'heatmap', color: '#ff0000', data: [{ label: '2026-01-01', value: 0 }, { label: '2026-01-02', value: 4 }] })])
    await nextTick()
    const cells = [...q(el, 'a2ui-chart').querySelectorAll('.rounded-sm')]
    expect(cells.some((c) => c.style.backgroundColor === 'rgb(255, 0, 0)')).toBe(true)
    expect(q(el, 'a2ui-chart').querySelector('[role="img"]').className).toContain('h-40')

    const hours = card([chart({ variant: 'heatmap', data: [{ label: '2026-01-01 09:00', value: 1 }] })])
    await nextTick()
    expect(q(hours, 'a2ui-chart').querySelector('[role="img"]').className).toContain('h-56')
    expect(q(hours, 'a2ui-chart').querySelector('[role="img"]').getAttribute('aria-label')).toBe('2026-01-01 09:00: 1')
  })

  it('says it has no points when none can be drawn', async () => {
    const el = card([chart({ variant: 'bar', data: { '@path': '/none' } })])
    await nextTick()
    expect(q(el, 'a2ui-chart').querySelector('[role="img"]').getAttribute('aria-label')).toBe('No data points')
    expect(q(el, 'a2ui-chart').textContent).toContain('No data points')
  })
})

describe('BarChart', () => {
  it('shows a tooltip over the bar under the pointer, placed to stay inside', async () => {
    const data = Array.from({ length: 10 }, (_, i) => ({ label: `d${i}`, count: i * 300 }))
    const el = mount(BarChart, { data, color: '#000000' })
    await nextTick()
    const bars = el.querySelectorAll('[data-test="bar"]')
    expect(el.textContent).toContain('2.7k')
    expect([...el.querySelectorAll('.h-5 span')].map((s) => s.textContent)).toEqual(['d0', 'd2', 'd4', 'd6', 'd8'])
    for (const [i, shift] of [[0, '0%'], [5, '-50%'], [9, '-100%']]) {
      bars[i].dispatchEvent(new Event('mouseenter'))
      await nextTick()
      const tip = el.querySelector('.z-20')
      expect(tip.textContent.trim()).toBe(`d${i}: ${i * 300}`)
      expect(tip.style.transform).toContain(`translate(${shift}`)
    }
    expect(bars[0].querySelector('div').className).toContain('opacity-60')
    bars[9].dispatchEvent(new Event('mouseleave'))
    await nextTick()
    expect(el.querySelector('.z-20')).toBeNull()
  })

  it('draws millions, and zeros as no bar at all', async () => {
    const el = mount(BarChart, { data: [{ label: 'a', count: 2500000 }, { label: 'b', count: 0 }] })
    await nextTick()
    expect(el.textContent).toContain('2.5M')
    expect(el.querySelectorAll('[data-test="bar"] div')[1].style.minHeight).toBe('0px')
    const zero = mount(BarChart, { data: [{ label: 'a', count: 0 }] })
    await nextTick()
    expect(zero.querySelector('[data-test="bar"] div').style.height).toBe('0%')
    const empty = mount(BarChart, {})
    expect(empty.textContent).toContain('No data points')
  })
})

describe('the analytics charts, for a card', () => {
  it("label a line chart's points with a card's own labels, and buckets as before", async () => {
    const el = mount(ChartSVG, { data: [{ date: 'Week 1', count: 1 }, { date: '2026-01-02', count: 2 }, { date: '2026-01-02 10:00', count: 3 }] })
    await nextTick()
    expect([...el.querySelectorAll('.h-5 span')].map((s) => s.textContent)).toEqual(['Week 1', '01-02', '10:00'])
  })

  it('shade a heatmap in a hex colour by level, over the empty grey', async () => {
    const el = mount(HeatmapChart, {
      data: [{ bucket: '2026-01-01', count: 1 }, { bucket: '2026-01-02', count: 2 }, { bucket: '2026-01-03', count: 3 }, { bucket: '2026-01-04', count: 4 }],
      rangeStart: new Date('2026-01-01T00:00:00').getTime() / 1000, rangeEnd: new Date('2026-01-04T00:00:00').getTime() / 1000, color: '#336699',
    })
    await nextTick()
    const colours = [...el.querySelectorAll('.rounded-sm')].map((c) => c.style.backgroundColor).filter(Boolean)
    expect(colours).toEqual(['rgba(51, 102, 153, 0.25)', 'rgba(51, 102, 153, 0.5)', 'rgba(51, 102, 153, 0.75)', 'rgb(51, 102, 153)'])
    expect(el.querySelectorAll('.bg-gray-100').length).toBeGreaterThan(0)
  })
})
