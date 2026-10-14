/**
 * A Pano doc is laid out by its own width, not the window's. Beside the
 * desktop app's side panel the view is a phone's width in a wide window, and a
 * window breakpoint there squeezed the note and its comments into one row
 * where a phone stacks them.
 */

import { describe, it, expect } from 'vitest'

import doc from '../src/views/PanoDocView.vue?raw'
import board from '../src/views/PanoView.vue?raw'

const template = (source) => source.slice(source.indexOf('<template>'))

describe('Pano layout', () => {
  it('measures the doc view against its own width', () => {
    expect(template(doc)).toMatch(/<template>\s*<div class="@container /)
  })

  it('puts the note and its comments side by side only when the view has room', () => {
    expect(template(doc)).toContain('class="grid gap-8 @min-[64rem]:grid-cols-[minmax(0,1fr)_320px] items-start"')
    expect(template(doc)).toContain('class="grid @min-[64rem]:grid-cols-[minmax(0,1fr)_340px]"')
  })

  it('asks no breakpoint of the window, but for the board chat box that follows the window-wide chat column', () => {
    expect(template(doc).match(/(?<=[\s"'])(sm|md|lg|xl|2xl):[\w[\]-]+/g)).toBeNull()
    expect(template(board).match(/(?<=[\s"'])(sm|md|lg|xl|2xl):[\w[\]-]+/g)).toEqual(['lg:block'])
  })
})
