// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The New Task form is laid out by its own width, not the window's. On the
 * desktop the form can be a phone's width in a wide window — beside the side
 * panel — and a window breakpoint there showed the Agent/Human toggle's words
 * where a phone shows its icons.
 */

import { describe, it, expect } from 'vitest'

import source from '../src/views/TaskFormView.vue?raw'

const template = source.slice(0, source.indexOf('<script'))

describe('TaskFormView layout', () => {
  it('is a container its breakpoints are measured against', () => {
    expect(template).toMatch(/<template>\s*(<!--[\s\S]*?-->\s*)?<div class="@container /)
  })

  it('asks no breakpoint of the window', () => {
    expect(template.match(/(?<=[\s"'])(sm|md|lg|xl|2xl):[\w[\]-]+/g)).toBeNull()
  })

  it('shows the toggle as icons until the form has room for the words', () => {
    expect(template).toContain('<span class="hidden @min-[40rem]:inline">Agent</span>')
    expect(template).toContain('<span class="hidden @min-[40rem]:inline">Human</span>')
  })
})
