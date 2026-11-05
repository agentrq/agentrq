// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'
import { nextTick, ref } from 'vue'

import {
  applyCommand,
  commandQuery,
  matchCommands,
  mergeCommands,
  skillCommands,
  useSkillCommands,
  useSlashCommands,
} from '../src/composables/useSlashCommands'

const COMMANDS = [
  { name: 'compact', description: 'Shorten the context' },
  { name: 'init', description: 'Set the project up' },
  { name: 'web', description: 'Search the web', hint: 'query to search for' },
]

/** The composable driven by a text ref, the way the composer drives it. */
function composer(text = '', commands = COMMANDS) {
  const textRef = ref(text)
  const commandsRef = ref(commands)
  return { textRef, commandsRef, menu: useSlashCommands({ text: textRef, commands: commandsRef }) }
}

describe('commandQuery', () => {
  it('reads the name being typed', () => {
    expect(commandQuery('/comp')).toBe('comp')
    expect(commandQuery('/')).toBe('')
  })

  it('is not asking for a command once the first token is finished', () => {
    // By then the command has been chosen and what follows is its argument.
    expect(commandQuery('/web agent protocol')).toBeNull()
    expect(commandQuery('/compact\n')).toBeNull()
  })

  it('is not asking for a command when the text does not start with one', () => {
    expect(commandQuery('please /compact')).toBeNull()
    expect(commandQuery(' /compact')).toBeNull()
    expect(commandQuery('')).toBeNull()
    expect(commandQuery(undefined)).toBeNull()
    expect(commandQuery(42)).toBeNull()
  })
})

describe('matchCommands', () => {
  it('offers everything for a bare slash', () => {
    expect(matchCommands(COMMANDS, '').map((c) => c.name)).toEqual(['compact', 'init', 'web'])
  })

  it('puts the commands that start with the query first', () => {
    // "pact" is inside "compact" but starts nothing, so a prefix match wins.
    const named = matchCommands([{ name: 'pact' }, { name: 'compact' }], 'pact')
    expect(named.map((c) => c.name)).toEqual(['pact', 'compact'])
  })

  it('still finds a command from the middle of its name', () => {
    expect(matchCommands(COMMANDS, 'pact').map((c) => c.name)).toEqual(['compact'])
  })

  it('keeps the order the agent gave within each group', () => {
    const ordered = matchCommands([{ name: 'ab' }, { name: 'aa' }], 'a')
    expect(ordered.map((c) => c.name)).toEqual(['ab', 'aa'])
  })

  it('matches without regard to case', () => {
    expect(matchCommands(COMMANDS, 'COMP').map((c) => c.name)).toEqual(['compact'])
  })

  it('offers nothing when nothing matches', () => {
    expect(matchCommands(COMMANDS, 'zzz')).toEqual([])
  })

  it('ignores entries that could never be invoked', () => {
    const messy = [null, undefined, {}, { name: '' }, { name: 7 }, { name: 'real' }]
    expect(matchCommands(messy, '').map((c) => c.name)).toEqual(['real'])
  })

  it('has nothing to offer when the agent advertised nothing', () => {
    expect(matchCommands(undefined, 'a')).toEqual([])
    expect(matchCommands([], 'a')).toEqual([])
  })
})

describe('applyCommand', () => {
  it('leaves a command that takes an argument ready for one', () => {
    expect(applyCommand({ name: 'web', hint: 'query' })).toBe('/web ')
    expect(applyCommand({ name: 'web', input: { hint: 'query' } })).toBe('/web ')
  })

  it('leaves a command that takes no argument ready to send', () => {
    expect(applyCommand({ name: 'compact' })).toBe('/compact')
    expect(applyCommand({ name: 'compact', hint: '' })).toBe('/compact')
  })
})

describe('useSlashCommands', () => {
  it('stays shut for an ordinary message', () => {
    const { menu } = composer('please compact the context')
    expect(menu.open.value).toBe(false)
    expect(menu.active.value).toBeNull()
  })

  it('opens on a slash and narrows as you type', async () => {
    const { textRef, menu } = composer('/')
    expect(menu.open.value).toBe(true)
    expect(menu.matches.value).toHaveLength(3)

    textRef.value = '/comp'
    await nextTick()
    expect(menu.matches.value.map((c) => c.name)).toEqual(['compact'])
  })

  it('never shows an empty menu', () => {
    // Covering the composer to say nothing is worse than not appearing.
    const { menu } = composer('/zzz')
    expect(menu.open.value).toBe(false)
  })

  it('shows no menu at all when the agent advertises nothing', () => {
    // Every agent that is not an ACP one, so nothing changes for them.
    expect(composer('/', []).menu.open.value).toBe(false)
    // `null` rather than `undefined`, which the helper's default would swallow.
    expect(composer('/', null).menu.open.value).toBe(false)
  })

  it('moves the highlight and wraps at both ends', () => {
    const { menu } = composer('/')
    expect(menu.active.value.name).toBe('compact')

    menu.move(1)
    expect(menu.active.value.name).toBe('init')

    menu.move(-1)
    menu.move(-1)
    expect(menu.active.value.name).toBe('web')

    menu.move(1)
    expect(menu.active.value.name).toBe('compact')
  })

  it('ignores movement while shut', () => {
    const { menu } = composer('hello')
    menu.move(1)
    expect(menu.selected.value).toBe(0)
  })

  it('has no highlighted command in the instant before the list settles', () => {
    // The reset runs on the next tick, so for one synchronous moment the
    // highlight can point past the end of a list that just got shorter.
    // Reading it then must give nothing rather than an undefined row.
    const { textRef, menu } = composer('/')
    menu.move(2)

    textRef.value = '/i'
    expect(menu.matches.value).toHaveLength(1)
    expect(menu.active.value).toBeNull()
  })

  it('highlights the best match again when the list changes underneath', async () => {
    const { textRef, menu } = composer('/')
    menu.move(2)
    expect(menu.selected.value).toBe(2)

    textRef.value = '/i'
    await nextTick()
    expect(menu.selected.value).toBe(0)
    expect(menu.active.value.name).toBe('init')
  })

  it('highlights a row the pointer names, and ignores one that does not exist', () => {
    const { menu } = composer('/')
    menu.highlight(2)
    expect(menu.active.value.name).toBe('web')

    menu.highlight(9)
    menu.highlight(-1)
    expect(menu.active.value.name).toBe('web')
  })

  it('accepts the highlighted command', () => {
    const { menu } = composer('/i')
    expect(menu.accept()).toBe('/init')
  })

  it('accepts a command named outright, for a click', () => {
    const { menu } = composer('/')
    expect(menu.accept(COMMANDS[2])).toBe('/web ')
  })

  it('closes over the command it just inserted', async () => {
    const { textRef, menu } = composer('/comp')
    textRef.value = menu.accept()
    await nextTick()

    expect(textRef.value).toBe('/compact')
    expect(menu.open.value).toBe(false)
  })

  it('has nothing to accept while shut, so the keystroke stays the caller"s', () => {
    // This is how Enter still sends an ordinary message.
    const { menu } = composer('hello')
    expect(menu.accept()).toBeNull()
    expect(composer('/zzz').menu.accept()).toBeNull()
  })

  it('accepts nothing when handed nothing', () => {
    const { menu } = composer('/')
    expect(menu.accept(null)).toBeNull()
  })

  it('stays shut once dismissed, until the text changes again', async () => {
    const { textRef, menu } = composer('/comp')
    menu.dismiss()
    await nextTick()
    expect(menu.open.value).toBe(false)

    // Changing one's mind brings it back, rather than the dismissal sticking
    // for the rest of the message.
    textRef.value = '/compa'
    await nextTick()
    expect(menu.open.value).toBe(true)
  })

  describe('handleKeydown', () => {
    const key = (k, mods = {}) => ({ key: k, ...mods })

    it('claims the arrow keys to move the highlight', () => {
      const { menu } = composer('/')
      expect(menu.handleKeydown(key('ArrowDown'))).toEqual({ text: null })
      expect(menu.active.value.name).toBe('init')
      expect(menu.handleKeydown(key('ArrowUp'))).toEqual({ text: null })
      expect(menu.active.value.name).toBe('compact')
    })

    it('claims Escape to shut the menu', async () => {
      const { menu } = composer('/comp')
      expect(menu.handleKeydown(key('Escape'))).toEqual({ text: null })
      await nextTick()
      expect(menu.open.value).toBe(false)
    })

    it('claims Enter and Tab to choose the highlighted command', () => {
      expect(composer('/i').menu.handleKeydown(key('Enter'))).toEqual({ text: '/init' })
      expect(composer('/i').menu.handleKeydown(key('Tab'))).toEqual({ text: '/init' })
    })

    it('leaves Cmd-Enter and Ctrl-Enter alone, so sending still sends', () => {
      // The composer's existing shortcut. A modifier means the keystroke was
      // never aimed at the menu.
      const { menu } = composer('/i')
      expect(menu.handleKeydown(key('Enter', { metaKey: true }))).toBeNull()
      expect(menu.handleKeydown(key('Enter', { ctrlKey: true }))).toBeNull()
    })

    it('leaves Shift-Enter alone, so it is still a newline', () => {
      expect(composer('/i').menu.handleKeydown(key('Enter', { shiftKey: true }))).toBeNull()
      expect(composer('/i').menu.handleKeydown(key('Enter', { altKey: true }))).toBeNull()
    })

    it('claims nothing at all while the menu is shut', () => {
      // Otherwise the arrow keys would stop moving the caret in an ordinary
      // message, which is what a `.prevent` in the markup would have done.
      const { menu } = composer('write me a haiku')
      for (const k of ['ArrowDown', 'ArrowUp', 'Escape', 'Enter', 'Tab']) {
        expect(menu.handleKeydown(key(k))).toBeNull()
      }
    })

    it('leaves ordinary typing alone', () => {
      expect(composer('/i').menu.handleKeydown(key('a'))).toBeNull()
    })
  })
})

describe('skills in the menu', () => {
  const SKILLS = [
    { name: 'release', description: 'Cut a release', workspaceEnabled: true },
    { name: 'off-here', workspaceEnabled: false },
    { name: 'off-account', enabled: false, workspaceEnabled: true },
    { name: 'bare', workspaceEnabled: true },
    { name: '', workspaceEnabled: true },
    null,
  ]

  it('offers only the skills the workspace\'s agents can load', () => {
    expect(skillCommands(SKILLS, 'ws1')).toEqual([
      { name: 'release', description: 'Cut a release', skill: true },
      { name: 'bare', description: '', skill: true },
    ])
    expect(skillCommands(undefined, 'ws1')).toEqual([])
  })

  it('leaves a space after a chosen skill, for what to do with it', () => {
    expect(applyCommand({ name: 'release', skill: true })).toBe('/release ')
  })

  it('merges lists, the first winning a name both have', () => {
    const agent = ref([{ name: 'review', description: 'agent' }, { name: '' }, null])
    const skills = [{ name: 'review', skill: true }, { name: 'release', skill: true }]
    expect(mergeCommands(agent, skills, undefined)).toEqual([
      { name: 'review', description: 'agent' },
      { name: 'release', skill: true },
    ])
  })

  it('loads the workspace\'s skills, and again when the workspace changes', async () => {
    const id = ref('ws1')
    const resolvers = {}
    const load = (ws) => new Promise((r) => { resolvers[ws] = r })
    const commands = useSkillCommands(id, { load })
    id.value = 'ws2'
    await nextTick()
    // The older answer arrives last and must not replace the newer one.
    resolvers.ws2({ skills: [{ name: 'two', workspaceEnabled: true }] })
    await nextTick()
    resolvers.ws1({ skills: [{ name: 'one', workspaceEnabled: true }] })
    await new Promise((r) => setTimeout(r))
    expect(commands.value.map((c) => c.name)).toEqual(['two'])
  })

  it('has no skills without a workspace, or when the load fails', async () => {
    const load = vi.fn().mockRejectedValue(new Error('down'))
    expect(useSkillCommands(ref(''), { load }).value).toEqual([])
    expect(load).not.toHaveBeenCalled()
    const failed = useSkillCommands('ws1', { load })
    await new Promise((r) => setTimeout(r))
    expect(failed.value).toEqual([])
  })

  it('reads them through the workspace by default', async () => {
    const api = await import('../src/api')
    const spy = vi.spyOn(api, 'searchWorkspaceSkills').mockResolvedValue({ skills: [{ name: 'x', workspaceEnabled: true }] })
    const commands = useSkillCommands('ws9')
    await new Promise((r) => setTimeout(r))
    expect(spy).toHaveBeenCalledWith('ws9')
    expect(commands.value.map((c) => c.name)).toEqual(['x'])
    spy.mockRestore()
  })
})
