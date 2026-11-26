// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A Pano doc's a2ui blocks: which a doc draws, their media resolved to the
 * doc's files, mounted into the rendered body and drawn in the note's
 * colours, and taken down again when the body changes.
 */

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

import { PANO_COMPONENTS, panoPieceTilt, panoSurface, panoTime } from '../src/a2ui/pano.js'
import { AGENTRQ_CATALOG_ID, BASIC_CATALOG_ID, validateMessages } from '../src/a2ui/validate.js'
import { mountPanoA2ui, usePanoA2ui } from '../src/composables/usePanoA2ui.js'
import { panoColor } from '../src/composables/usePano.js'
import { renderMarkdown } from '../src/utils/markdown.js'

setActivePinia(createPinia())

const FILES = [
  { filename: 'shot.png', url: '/storage/pano/u-1/pano-2/shot.png' },
  { filename: 'demo.webm', url: 'https://bucket.example/pano/demo.webm' },
  { filename: 'talk.webm', url: '/storage/pano/u-1/pano-2/talk.webm' },
]
const BLUE = panoColor('blue')

const messages = (components, catalogId = AGENTRQ_CATALOG_ID) => [
  { version: '1.0', createSurface: { surfaceId: 's1', catalogId } },
  { version: '1.0', updateComponents: { surfaceId: 's1', components } },
]
const source = (components, catalogId) => JSON.stringify(messages(components, catalogId))
const fence = (components) => '```a2ui\n' + source(components) + '\n```'

const q = (el, test) => el.querySelector(`[data-test="${test}"]`)
const qa = (el, test) => [...el.querySelectorAll(`[data-test="${test}"]`)]

const containers = []
// The doc's body as the page renders it, with its blocks' cards mounted.
function body(markdown, files = FILES, color = BLUE) {
  const el = document.createElement('div')
  el.innerHTML = renderMarkdown(markdown)
  document.body.appendChild(el)
  const unmount = mountPanoA2ui(el, { files, color })
  containers.push(unmount)
  return { el, unmount }
}

afterEach(() => {
  while (containers.length) containers.pop()()
  document.body.innerHTML = ''
})

describe('panoSurface', () => {
  it('draws a doc component, its media the doc\'s file', () => {
    const s = panoSurface(source([{ id: 'root', component: 'Image', url: 'attachment:shot.png' }]), FILES)
    expect(s.components.get('root').url).toBe('/storage/pano/u-1/pano-2/shot.png')
  })

  it('draws nothing that is not a doc\'s card', () => {
    expect(panoSurface('not json', FILES)).toBeNull()
    expect(panoSurface('{"a":1}', FILES)).toBeNull()
    // Not a file of the doc, or a link: the check refuses it.
    expect(panoSurface(source([{ id: 'root', component: 'Image', url: 'attachment:other.png' }]), FILES)).toBeNull()
    expect(panoSurface(source([{ id: 'root', component: 'Image', url: { '@path': '/x' } }]), FILES)).toBeNull()
    // Progress is the checklist's, List a table's, and nothing answers a doc.
    for (const c of [{ component: 'Progress', value: 1 }, { component: 'List', children: [] }, { component: 'Button', child: 'root', action: { event: { name: 'x' } } }]) {
      expect(panoSurface(source([{ id: 'root', ...c }]), FILES)).toBeNull()
    }
    // A deleted surface draws nothing.
    expect(panoSurface(JSON.stringify([...messages([{ id: 'root', component: 'Text', text: 'x' }]), { version: '1.0', deleteSurface: { surfaceId: 's1' } }]), FILES)).toBeNull()
  })

  it('resolves media in createSurface and leaves what is no doc file', () => {
    const msgs = [{ version: '1.0', createSurface: { surfaceId: 's1', catalogId: BASIC_CATALOG_ID, components: [
      { id: 'root', component: 'Video', url: 'attachment:demo.webm', posterUrl: 'attachment:shot.png' },
    ] } }, { version: '1.0', updateDataModel: { surfaceId: 's1', path: '/', value: {} } }, null]
    expect(panoSurface(JSON.stringify(msgs), null)).toBeNull()
    const s = panoSurface(JSON.stringify(msgs.slice(0, 2)), FILES)
    expect(s.components.get('root')).toMatchObject({ url: FILES[1].url, posterUrl: FILES[0].url })
  })

  it('names what a doc draws as the server does', () => {
    expect(PANO_COMPONENTS).toEqual(['Text', 'Row', 'Column', 'Card', 'Tabs', 'Divider', 'Icon', 'Image', 'Video', 'AudioPlayer', 'Chart'])
    expect(validateMessages(messages([{ id: 'root', component: 'Progress', value: 1 }]), { components: PANO_COMPONENTS }).ok).toBe(false)
    expect(validateMessages(messages([{ id: 'root', component: 'Progress', value: 1 }])).ok).toBe(true)
  })
})

describe('panoPieceTilt and panoTime', () => {
  it('leans a piece by its key, the same each time', () => {
    expect(panoPieceTilt('s1card')).toBe(panoPieceTilt('s1card'))
    expect(Math.abs(panoPieceTilt(undefined))).toBeLessThanOrEqual(1)
  })

  it('says a time as m:ss, and nothing it cannot read as 0:00', () => {
    expect(panoTime(75.9)).toBe('1:15')
    expect(panoTime(Infinity)).toBe('0:00')
    expect(panoTime(-1)).toBe('0:00')
    expect(panoTime(NaN)).toBe('0:00')
  })
})

describe('mountPanoA2ui', () => {
  it('draws each valid block in place of its code, beside the markdown', () => {
    const { el, unmount } = body(`# Doc\n\n- [ ] one\n\n${fence([{ id: 'root', component: 'Text', text: 'Hello' }])}\n\n\`\`\`a2ui\nnot a card\n\`\`\`\n\n\`\`\`js\nx\n\`\`\``)
    expect(qa(el, 'pano-a2ui')).toHaveLength(1)
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('Hello')
    const pres = [...el.querySelectorAll('pre')]
    expect(pres.map((p) => p.hidden)).toEqual([true, false, false])
    expect(el.querySelector('input[type="checkbox"]')).not.toBeNull()
    unmount()
    expect(qa(el, 'pano-a2ui')).toHaveLength(0)
    expect(el.querySelector('[data-pano-a2ui-host]')).toBeNull()
    expect(pres[0].hidden).toBe(false)
  })

  it('has nothing to draw without a body', () => {
    expect(() => mountPanoA2ui(null, { files: [], color: BLUE })()).not.toThrow()
  })
})

describe('usePanoA2ui', () => {
  it('draws the blocks again when the page renders the body again, and takes them down with the page', async () => {
    const html = ref(renderMarkdown(fence([{ id: 'root', component: 'Text', text: 'One' }])))
    const color = ref(BLUE)
    const Page = defineComponent({
      setup() {
        const el = ref(null)
        usePanoA2ui(() => el.value, () => ({ html: html.value, files: FILES, color: color.value }))
        return () => h('div', { ref: el, innerHTML: html.value })
      },
    })
    const root = document.createElement('div')
    document.body.appendChild(root)
    const app = createApp(Page)
    app.mount(root)
    await nextTick()
    await nextTick()
    expect(q(root, 'a2ui-text').textContent.trim()).toBe('One')

    // The same HTML, another colour: the body is not rewritten, and the card is drawn again.
    color.value = panoColor('pink')
    await nextTick()
    await nextTick()
    expect(qa(root, 'pano-a2ui')).toHaveLength(1)
    expect(q(root, 'pano-a2ui').style.getPropertyValue('--pano-accent')).toBe(panoColor('pink').inkHex)

    html.value = renderMarkdown(fence([{ id: 'root', component: 'Text', text: 'Two' }]))
    await nextTick()
    await nextTick()
    expect(qa(root, 'a2ui-text').map((t) => t.textContent.trim())).toEqual(['Two'])
    app.unmount()
    expect(root.querySelector('[data-pano-a2ui-host]')).toBeNull()
  })
})

describe('the note\'s look', () => {
  it('tapes a card to the note, leaning', () => {
    const { el } = body(fence([{ id: 'root', component: 'Card', child: 't' }, { id: 't', component: 'Text', text: 'x', variant: 'caption' }]))
    const card = q(el, 'a2ui-card')
    expect(card.dataset.pano).toBe('true')
    expect(card.style.transform).toMatch(/^rotate\(-?[\d.]+deg\)$/)
    expect(card.querySelector('span[aria-hidden="true"]').className).toContain(BLUE.tape)
    expect(q(el, 'a2ui-text').className).toContain(BLUE.muted)
  })

  it('draws a body text in the note\'s ink', () => {
    const { el } = body(fence([{ id: 'root', component: 'Text', text: 'x' }]))
    expect(q(el, 'a2ui-text').className).toContain('text-[14px]')
    expect(q(el, 'a2ui-text').className).not.toContain('text-gray-800')
  })

  it('makes tabs the note\'s tag pills', async () => {
    const { el } = body(fence([
      { id: 'root', component: 'Tabs', tabs: [{ title: 'One', child: 'a' }, { title: 'Two', child: 'b' }] },
      { id: 'a', component: 'Text', text: 'first' }, { id: 'b', component: 'Text', text: 'second' },
    ]))
    expect(q(el, 'a2ui-tabs').dataset.pano).toBe('true')
    expect(q(el, 'a2ui-tab-0').getAttribute('aria-selected')).toBe('true')
    expect(q(el, 'a2ui-tab-0').className).toContain(BLUE.tag)
    q(el, 'a2ui-tab-1').click()
    await nextTick()
    expect(q(el, 'a2ui-tab-1').getAttribute('aria-selected')).toBe('true')
    expect(q(el, 'a2ui-tab-0').className).toContain(BLUE.muted)
    expect(q(el, 'a2ui-text').textContent.trim()).toBe('second')
  })

  it('tapes a photo, but leaves an icon-sized image as it is', () => {
    const { el } = body(fence([
      { id: 'root', component: 'Column', children: ['a', 'b'] },
      { id: 'a', component: 'Image', url: 'attachment:shot.png' },
      { id: 'b', component: 'Image', url: 'attachment:shot.png', variant: 'icon' },
    ]))
    const [photo, icon] = qa(el, 'a2ui-image')
    expect(photo.closest('figure').dataset.pano).toBe('true')
    expect(photo.getAttribute('src')).toBe(FILES[0].url)
    expect(icon.closest('figure')).toBeNull()
  })

  it('plays a video from the note\'s play button', async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(() => Promise.resolve())
    const { el } = body(fence([{ id: 'root', component: 'Video', url: 'attachment:demo.webm' }]))
    const video = q(el, 'a2ui-video')
    expect(video.getAttribute('src')).toBe(FILES[1].url)
    expect(video.hasAttribute('controls')).toBe(false)
    expect(q(el, 'a2ui-video-play').className).toContain(BLUE.inkBg)
    q(el, 'a2ui-video-play').click()
    await nextTick()
    expect(play).toHaveBeenCalled()
    expect(video.hasAttribute('controls')).toBe(true)
    expect(q(el, 'a2ui-video-play')).toBeNull()
    play.mockRestore()
  })

  it('shows the controls of a video played some other way', async () => {
    const { el } = body(fence([{ id: 'root', component: 'Video', url: 'attachment:demo.webm' }]))
    q(el, 'a2ui-video').dispatchEvent(new Event('play'))
    await nextTick()
    expect(q(el, 'a2ui-video-play')).toBeNull()
  })

  it('plays, pauses and seeks the pill audio player', async () => {
    const { el } = body(fence([{ id: 'root', component: 'AudioPlayer', url: 'attachment:talk.webm', description: 'Standup' }]))
    const audio = q(el, 'a2ui-audio')
    expect(el.textContent).toContain('Standup')
    let paused = true
    Object.defineProperty(audio, 'paused', { get: () => paused })
    audio.play = vi.fn(() => { paused = false; audio.dispatchEvent(new Event('play')) })
    audio.pause = vi.fn(() => { paused = true; audio.dispatchEvent(new Event('pause')) })
    const button = q(el, 'a2ui-audio-play')
    expect(button.getAttribute('aria-label')).toBe('Play')
    button.click()
    await nextTick()
    expect(button.getAttribute('aria-label')).toBe('Pause')
    button.click()
    await nextTick()
    expect(button.getAttribute('aria-label')).toBe('Play')
    audio.dispatchEvent(new Event('ended'))

    // Seeking waits for a length; then a click on the bar moves there.
    const bar = q(el, 'a2ui-audio-bar')
    bar.getBoundingClientRect = () => ({ left: 0, width: 200 })
    bar.dispatchEvent(new MouseEvent('click', { clientX: 50, bubbles: true }))
    expect(audio.currentTime).toBe(0)
    Object.defineProperty(audio, 'duration', { value: 80, configurable: true })
    audio.dispatchEvent(new Event('loadedmetadata'))
    bar.dispatchEvent(new MouseEvent('click', { clientX: 50, bubbles: true }))
    expect(audio.currentTime).toBe(20)
    audio.dispatchEvent(new Event('timeupdate'))
    await nextTick()
    expect(q(el, 'a2ui-audio-time').textContent).toBe('0:20 / 1:20')
    expect(bar.firstElementChild.style.width).toBe('25%')
  })

  it('shows an audio player with no length at its start', () => {
    const { el } = body(fence([{ id: 'root', component: 'AudioPlayer', url: 'attachment:talk.webm' }]))
    expect(q(el, 'a2ui-audio-time').textContent).toBe('0:00 / 0:00')
    expect(q(el, 'a2ui-audio-bar').firstElementChild.style.width).toBe('0%')
  })

  it('puts an icon on the note\'s tag, and dashes dividers in its ink', () => {
    const { el } = body(fence([
      { id: 'root', component: 'Row', children: ['i', 'v'] },
      { id: 'i', component: 'Icon', name: 'check' },
      { id: 'v', component: 'Divider', axis: 'vertical' },
    ]) + '\n\n' + fence([
      { id: 'root', component: 'Column', children: ['h', 'i'] },
      { id: 'h', component: 'Divider' },
      { id: 'i', component: 'Icon', name: { svgPath: 'M0 0h24v24H0z' } },
    ]))
    const [check, custom] = qa(el, 'a2ui-icon')
    expect(check.parentElement.className).toContain(BLUE.tag)
    expect(check.querySelector('path').getAttribute('stroke')).toBe('currentColor')
    expect(custom.querySelector('path').getAttribute('fill')).toBe('currentColor')
    const [vertical, across] = qa(el, 'a2ui-divider')
    expect(vertical.getAttribute('aria-orientation')).toBe('vertical')
    expect(vertical.className).toContain('border-dashed')
    expect(across.tagName).toBe('HR')
    expect(across.className).toContain('border-dashed')
  })

  it('draws a chart in the note\'s ink, unless it has a colour of its own', () => {
    const { el } = body(fence([
      { id: 'root', component: 'Column', children: ['a', 'b'] },
      { id: 'a', component: 'Chart', variant: 'bar', label: 'PRs', data: [{ label: 'Mon', value: 2 }] },
      { id: 'b', component: 'Chart', variant: 'bar', label: 'Own', color: '#123456', data: [{ label: 'Mon', value: 2 }] },
    ]))
    const [ink, own] = qa(el, 'a2ui-chart')
    const dot = (chart) => chart.querySelector('figcaption span').style.backgroundColor
    expect(dot(ink)).toBe('rgb(37, 99, 235)') // #2563eb, the blue note's ink
    expect(dot(own)).toBe('rgb(18, 52, 86)')
    expect(ink.querySelector('figcaption span:last-child').className).toContain(BLUE.title)
  })
})
