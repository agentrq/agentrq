// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it } from 'vitest'

import { ICON_PATHS, iconDrawing, iconFor } from '../src/a2ui/icons.js'
import {
  accessibilityAttrs, buttonClass, chipClass, choiceRowClass, displayText, flexClasses, imageClasses, isAllowedMedia, listClasses,
  textHtml, weightStyle,
} from '../src/a2ui/render.js'
import { CANCEL_BUTTON, PRIMARY_BUTTON, SECONDARY_BUTTON } from '../src/utils/formStyles.js'
import { ICON_NAMES } from '../src/a2ui/validate.js'

const ctx = (dataModel = {}) => ({ surface: { dataModel }, scope: [] })

describe('a2ui layout classes', () => {
  it('maps justify and align, with the spec defaults', () => {
    expect(flexClasses({})).toEqual(['justify-start', 'items-stretch'])
    expect(flexClasses({ justify: 'spaceBetween', align: 'center' })).toEqual(['justify-between', 'items-center'])
    expect(flexClasses({ justify: 'spaceAround', align: 'end' })).toEqual(['justify-around', 'items-end'])
    expect(flexClasses({ justify: 'spaceEvenly', align: 'start' })).toEqual(['justify-evenly', 'items-start'])
    expect(flexClasses({ justify: 'stretch' })[0]).toBe('justify-stretch')
    expect(flexClasses({ justify: 'center' })[0]).toBe('justify-center')
    expect(flexClasses({ justify: 'end' })[0]).toBe('justify-end')
    expect(flexClasses({ justify: 'sideways', align: 'nowhere' })).toEqual(['justify-start', 'items-stretch'])
  })

  it('lays a List out by its direction', () => {
    expect(listClasses({})).toEqual(['flex-col', 'items-stretch'])
    expect(listClasses({ direction: 'horizontal', align: 'center' })).toEqual(['flex-row overflow-x-auto', 'items-center'])
  })

  it('turns a positive weight into a flex share', () => {
    expect(weightStyle({ weight: 2 })).toEqual({ flex: '2 1 0%', minWidth: 0 })
    expect(weightStyle({ weight: 0 })).toBeUndefined()
    expect(weightStyle({})).toBeUndefined()
    expect(weightStyle(undefined)).toBeUndefined()
  })

  it('sizes and fits an Image by its variant and fit', () => {
    expect(imageClasses({})).toEqual(['w-full max-w-80', 'object-fill'])
    expect(imageClasses({ variant: 'header' })).toEqual(['w-full max-h-48', 'object-cover'])
    expect(imageClasses({ variant: 'avatar' })).toEqual(['w-10 h-10 rounded-full', 'object-cover'])
    expect(imageClasses({ variant: 'icon', fit: 'contain' })).toEqual(['w-6 h-6', 'object-contain'])
    expect(imageClasses({ variant: 'smallFeature', fit: 'scaleDown' })).toEqual(['w-full max-w-32', 'object-scale-down'])
    expect(imageClasses({ variant: 'largeFeature', fit: 'none' })[1]).toBe('object-none')
    expect(imageClasses({ variant: 'mediumFeature', fit: 'cover' })[1]).toBe('object-cover')
  })
})

describe('a2ui accessibility', () => {
  it('resolves label and description, and passes live and hidden', () => {
    const c = { '@accessibility': { label: { '@path': '/l' }, description: 'More', live: 'polite', hidden: true } }
    expect(accessibilityAttrs(c, ctx({ l: 'Status' }))).toEqual({
      'aria-label': 'Status', 'aria-description': 'More', 'aria-live': 'polite', 'aria-hidden': 'true',
    })
  })

  it('leaves out what is empty, off or not a string', () => {
    const c = { '@accessibility': { label: '', description: { '@path': '/n' }, live: 'off', hidden: false } }
    expect(accessibilityAttrs(c, ctx({ n: 3 }))).toEqual({})
    expect(accessibilityAttrs({ '@accessibility': { live: 'assertive' } }, ctx())).toEqual({ 'aria-live': 'assertive' })
    expect(accessibilityAttrs({}, ctx())).toEqual({})
    expect(accessibilityAttrs(undefined, ctx())).toEqual({})
  })
})

describe('a2ui text', () => {
  it('shows strings, numbers and booleans, and nothing else', () => {
    expect(displayText('a')).toBe('a')
    expect(displayText(3)).toBe('3')
    expect(displayText(false)).toBe('false')
    expect(displayText(null)).toBe('')
    expect(displayText(undefined)).toBe('')
    expect(displayText({ a: 1 })).toBe('')
  })

  it('renders markdown but no images or links', () => {
    const html = textHtml('## Title\n**bold** ![x](https://tracker.example/p.png) [site](https://example.com) <b>raw</b>')
    expect(html).toContain('<h2')
    expect(html).toContain('<strong>bold</strong>')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('<a')
    expect(html).toContain('site')
    expect(html).not.toContain('<b>')
  })
})

describe('a2ui media', () => {
  const attachments = [{ url: 'https://bucket.example/w/t/a1' }, { url: '' }, {}]

  it('allows our own relative routes', () => {
    expect(isAllowedMedia('/storage/artifacts/w/t/a1')).toBe(true)
    expect(isAllowedMedia('/api/v1/workspaces/w/tasks/t/attachments/a1')).toBe(true)
  })

  it('allows a bucket link only when it is one of the message attachments', () => {
    expect(isAllowedMedia('https://bucket.example/w/t/a1', attachments)).toBe(true)
    expect(isAllowedMedia('https://bucket.example/w/t/other', attachments)).toBe(false)
    expect(isAllowedMedia('https://bucket.example/w/t/a1')).toBe(false)
    expect(isAllowedMedia('https://bucket.example/w/t/a1', null)).toBe(false)
  })

  it('refuses anything else', () => {
    for (const url of ['', undefined, 3, 'http://x/y', 'javascript:alert(1)', 'data:image/png;base64,AA', '//evil.example/storage/x',
      '/storage/../api/v1/user', '/other/x', 'storage/x']) {
      expect(isAllowedMedia(url, attachments)).toBe(false)
    }
  })
})

describe('a2ui icons', () => {
  it('draws every catalog name, and no other', () => {
    expect(Object.keys(ICON_PATHS).sort()).toEqual([...ICON_NAMES].sort())
    for (const name of ICON_NAMES) expect(ICON_PATHS[name]).toMatch(/^M[-\d. a-zA-Z]+$/)
  })

  it('tells a catalog name from a custom path', () => {
    expect(iconDrawing('check')).toEqual({ stroke: ICON_PATHS.check })
    expect(iconDrawing('toString')).toBeNull()
    expect(iconDrawing({ svgPath: 'M0 0h24v24z' })).toEqual({ fill: 'M0 0h24v24z' })
    expect(iconDrawing({ svgPath: '' })).toBeNull()
    expect(iconDrawing(null)).toBeNull()
    expect(iconDrawing(7)).toBeNull()
  })

  it('resolves a bound name and a bound svgPath', () => {
    const c = ctx({ icon: 'star', path: 'M1 1z', custom: { svgPath: 'M2 2z' } })
    expect(iconFor({ name: { '@path': '/icon' } }, c)).toEqual({ stroke: ICON_PATHS.star })
    expect(iconFor({ name: { svgPath: { '@path': '/path' } } }, c)).toEqual({ fill: 'M1 1z' })
    expect(iconFor({ name: { '@path': '/custom' } }, c)).toEqual({ fill: 'M2 2z' })
    expect(iconFor({ name: { '@path': '/missing' } }, c)).toBeNull()
  })
})

describe('a2ui control classes', () => {
  const classes = (list) => list.join(' ').split(/\s+/).sort()
  const own = (s) => s.split(/\s+/).sort()

  it("draws a Button as the app's own primary, secondary or borderless button", () => {
    expect(classes(buttonClass('primary')), 'primary').toEqual(own(PRIMARY_BUTTON))
    expect(classes(buttonClass('borderless')), 'borderless').toEqual(own(CANCEL_BUTTON))
    expect(classes(buttonClass(undefined)), 'no variant').toEqual(own(SECONDARY_BUTTON))
  })

  it('keeps the Button that answered its card at full strength, though disabled', () => {
    expect(classes(buttonClass('primary', true)), 'the chosen one').not.toContain('disabled:opacity-30')
    expect(classes(buttonClass('primary', true)), 'the chosen one').toContain('disabled:cursor-default')
  })

  it('fills a picked chip and fades an unpicked one only when it cannot be picked', () => {
    expect(classes(chipClass(true)), 'a picked chip, black as the primary button').toContain('bg-black')
    expect(classes(chipClass(true)), 'a picked chip').not.toContain('disabled:opacity-50')
    expect(classes(chipClass(false)), 'an unpicked chip').toContain('disabled:opacity-50')
  })

  it('marks a picked row, and on a closed card fades only the rows not picked', () => {
    expect(classes(choiceRowClass(true, false)), 'picked, open').toEqual(expect.arrayContaining(['border-gray-900', 'cursor-pointer']))
    expect(classes(choiceRowClass(false, false)), 'unpicked, open').toEqual(expect.arrayContaining(['border-gray-300', 'cursor-pointer']))
    expect(classes(choiceRowClass(true, true)), 'picked, closed').not.toContain('opacity-50')
    expect(classes(choiceRowClass(false, true)), 'unpicked, closed').toContain('opacity-50')
  })

  it('makes every control the height of a field: a 20px line and 10px padding', () => {
    for (const [name, list] of [['button', buttonClass('primary')], ['chip', chipClass(false)], ['row', choiceRowClass(false, false)]]) {
      expect(classes(list), name).toEqual(expect.arrayContaining(['leading-5', 'py-2.5', 'border', 'pointer-coarse:min-h-11']))
    }
  })
})
