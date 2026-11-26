// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { unique, watchTraffic } from '../lib/app.js'

// A Pano doc's a2ui blocks: the server keeps the files a block shows and
// refuses a block a doc cannot draw, and the doc page draws each block as a
// card in the note's own colours, beside its markdown. With QA_SCREENSHOTS
// set to a folder, it keeps a picture of the doc in each colour there.

const API = '/api/v1'
const CATALOG = 'https://agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json'

async function ok(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.json()
}

// A block drawing components, from the one with id root.
const block = (id, components) => '```a2ui\n' + JSON.stringify([
  { version: '1.0', createSurface: { surfaceId: id, catalogId: CATALOG } },
  { version: '1.0', updateComponents: { surfaceId: id, components } },
]) + '\n```'

const days = (n) => Array.from({ length: n }, (_, i) => {
  const d = new Date(Date.UTC(2026, 9, 1 + i))
  return { label: d.toISOString().slice(0, 10), value: (i * 7) % 5 + (i % 3 === 0 ? 3 : 0) }
})

const MARKDOWN = [
  '# Release 0.20',
  '',
  'Rich content sits beside the doc\'s own markdown, checklist and tables.',
  '',
  '- [x] Ship the backend',
  '- [ ] Record the demo',
  '',
  block('summary', [
    { id: 'root', component: 'Card', child: 'col' },
    { id: 'col', component: 'Column', children: ['head', 'rule', 'checks', 'note'] },
    { id: 'head', component: 'Text', text: '## Release health' },
    { id: 'rule', component: 'Divider' },
    { id: 'checks', component: 'Row', children: ['ok', 'okText', 'warn', 'warnText'], align: 'center' },
    { id: 'ok', component: 'Icon', name: 'check' },
    { id: 'okText', component: 'Text', text: '**412** tests passed' },
    { id: 'warn', component: 'Icon', name: 'warning' },
    { id: 'warnText', component: 'Text', text: '**2** flaky on CI' },
    { id: 'note', component: 'Text', variant: 'caption', text: 'Updated by the release agent' },
  ]),
  '',
  '## Numbers',
  '',
  block('numbers', [
    { id: 'root', component: 'Tabs', tabs: [{ title: 'Merged', child: 'bar' }, { title: 'Trend', child: 'line' }, { title: 'Activity', child: 'heat' }] },
    { id: 'bar', component: 'Chart', variant: 'bar', label: 'PRs merged', data: [{ label: 'Mon', value: 2 }, { label: 'Tue', value: 5 }, { label: 'Wed', value: 1 }, { label: 'Thu', value: 4 }, { label: 'Fri', value: 3 }] },
    { id: 'line', component: 'Chart', variant: 'line', label: 'Open issues', data: days(14) },
    { id: 'heat', component: 'Chart', variant: 'heatmap', label: 'Commits', data: days(56) },
  ]),
  '',
  '## Media',
  '',
  block('media', [
    { id: 'root', component: 'Column', children: ['photo', 'video', 'audio'] },
    { id: 'photo', component: 'Image', url: 'attachment:shot.png', variant: 'mediumFeature' },
    { id: 'video', component: 'Video', url: 'attachment:demo.webm' },
    { id: 'audio', component: 'AudioPlayer', url: 'attachment:standup.webm', description: 'Standup notes' },
  ]),
  '',
  '| Area | Owner |',
  '| --- | --- |',
  '| Backend | Ada |',
].join('\n')

// A picture, a video and a recording, made in the browser: a PNG from a
// canvas, and WebM from MediaRecorder, which Chromium plays.
async function makeMedia(page) {
  return page.evaluate(async () => {
    const canvas = document.createElement('canvas')
    canvas.width = 480
    canvas.height = 270
    const g = canvas.getContext('2d')
    const draw = (t) => {
      const grad = g.createLinearGradient(0, 0, 480, 270)
      grad.addColorStop(0, '#0ea5e9')
      grad.addColorStop(1, '#a855f7')
      g.fillStyle = grad
      g.fillRect(0, 0, 480, 270)
      g.fillStyle = 'rgba(255,255,255,0.9)'
      g.beginPath()
      g.arc(80 + t * 320, 135, 40, 0, Math.PI * 2)
      g.fill()
      g.font = 'bold 34px sans-serif'
      g.fillText('agentrq 0.20', 150, 70)
    }
    draw(0.5)
    const png = canvas.toDataURL('image/png').split(',')[1]
    const b64 = async (blob) => {
      const bytes = new Uint8Array(await blob.arrayBuffer())
      let s = ''
      for (const b of bytes) s += String.fromCharCode(b)
      return btoa(s)
    }
    const record = (stream, ms, tick) => new Promise((resolve) => {
      const rec = new MediaRecorder(stream)
      const parts = []
      rec.ondataavailable = (e) => parts.push(e.data)
      rec.onstop = () => resolve(new Blob(parts, { type: rec.mimeType }))
      rec.start()
      const start = performance.now()
      const step = () => {
        const t = (performance.now() - start) / ms
        if (t >= 1) return rec.stop()
        tick?.(t)
        requestAnimationFrame(step)
      }
      step()
    })
    const video = await record(canvas.captureStream(25), 1500, draw)
    const audio = new AudioContext()
    const osc = audio.createOscillator()
    const out = audio.createMediaStreamDestination()
    osc.connect(out)
    osc.start()
    const sound = await record(out.stream, 1500)
    osc.stop()
    return { png, video: await b64(video), audio: await b64(sound) }
  })
}

test('a Pano doc draws its a2ui blocks as cards in the note\'s colours', async ({ page }) => {
  const traffic = watchTraffic(page)
  await page.goto('/pano')
  const media = await makeMedia(page)
  const files = [
    { filename: 'shot.png', mimeType: 'image/png', data: media.png },
    { filename: 'demo.webm', mimeType: 'video/webm', data: media.video },
    { filename: 'standup.webm', mimeType: 'audio/webm', data: media.audio },
  ]

  // A block a doc cannot draw is refused, naming its line and what it may use.
  const refused = await page.request.post(`${API}/pano`, { data: { title: unique('Refused'), markdown: block('x', [{ id: 'root', component: 'Progress', value: 1 }]) } })
  expect(refused.status()).toBe(422)
  expect(await refused.text()).toContain('the a2ui block at line 1')

  const docs = {}
  for (const color of ['yellow', 'blue', 'pink', 'green']) {
    docs[color] = (await ok(await page.request.post(`${API}/pano`, { data: { title: unique(`Release 0.20 ${color}`), color, markdown: MARKDOWN, files } }), `saving the ${color} doc`)).doc
  }
  // The files only the blocks link are kept.
  expect(docs.yellow.files.map((f) => f.filename).sort()).toEqual(['demo.webm', 'shot.png', 'standup.webm'])

  await page.setViewportSize({ width: 900, height: 1000 })
  await page.goto(`/pano/${docs.yellow.id}`)
  const body = page.getByTestId('pano-body')
  await expect(body.getByTestId('pano-a2ui')).toHaveCount(3)
  // The blocks' code is hidden behind their cards.
  await expect(body.locator('pre:visible')).toHaveCount(0)
  // The checklist beside the cards still counts.
  await expect(page.getByTestId('pano-progress')).toContainText('1 of 2 done')

  const card = body.getByTestId('a2ui-card')
  await expect(card).toHaveAttribute('data-pano', 'true')
  await expect(card).toContainText('412 tests passed')
  await expect(body.getByTestId('a2ui-icon')).toHaveCount(2)
  await expect(body.getByTestId('a2ui-divider')).toHaveCount(1)

  // Tabs are the note's tag pills; each draws its chart.
  await expect(body.getByTestId('a2ui-chart')).toHaveAttribute('data-variant', 'bar')
  await body.getByTestId('a2ui-tab-1').click()
  await expect(body.getByTestId('a2ui-chart')).toHaveAttribute('data-variant', 'line')
  await body.getByTestId('a2ui-tab-2').click()
  await expect(body.getByTestId('a2ui-chart')).toHaveAttribute('data-variant', 'heatmap')
  await body.getByTestId('a2ui-tab-0').click()

  // Media plays from the doc's own files.
  const shot = body.getByTestId('a2ui-image')
  await expect(shot).toHaveAttribute('src', docs.yellow.files.find((f) => f.filename === 'shot.png').url)
  await expect.poll(() => shot.evaluate((img) => img.naturalWidth)).toBe(480)
  await body.getByTestId('a2ui-video-play').click()
  await expect(body.getByTestId('a2ui-video')).toHaveAttribute('controls', '')
  await body.getByTestId('a2ui-video').evaluate((v) => { v.pause(); v.currentTime = 0.7 })
  await body.getByTestId('a2ui-audio-play').click()
  await expect(body.getByTestId('a2ui-audio-play')).toHaveAttribute('aria-label', 'Pause')
  await body.getByTestId('a2ui-audio-play').click()

  const shots = process.env.QA_SCREENSHOTS
  if (shots) {
    // Tall enough for the whole note: the page scrolls inside its own pane.
    await page.setViewportSize({ width: 900, height: 3000 })
    const article = page.locator('article').first()
    await article.screenshot({ path: `${shots}/pano-a2ui-yellow.png` })
    for (const color of ['blue', 'pink', 'green']) {
      await page.goto(`/pano/${docs[color].id}`)
      await expect(page.getByTestId('pano-a2ui')).toHaveCount(3)
      await page.getByTestId('a2ui-tab-2').click()
      await expect(page.getByTestId('a2ui-chart')).toHaveAttribute('data-variant', 'heatmap')
      await page.locator('article').first().screenshot({ path: `${shots}/pano-a2ui-${color}.png` })
    }
    await page.emulateMedia({ colorScheme: 'dark' })
    await page.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
    await page.goto(`/pano/${docs.blue.id}`)
    await expect(page.getByTestId('pano-a2ui')).toHaveCount(3)
    await page.getByTestId('a2ui-tab-1').click()
    await page.locator('article').first().screenshot({ path: `${shots}/pano-a2ui-blue-dark.png` })
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.screenshot({ path: `${shots}/pano-a2ui-blue-dark-page.png` })
  }
  traffic.expectClean()
})
