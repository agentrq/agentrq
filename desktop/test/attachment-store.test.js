import { describe, it, expect } from 'vitest'

import { createAttachmentStore, parseAttachmentPath } from '../src/main/attachment-store.js'

const path = (ws, att) => `/api/v1/workspaces/${ws}/tasks/0iCYTqxKOqv/attachments/${att}`
const A = path('0iCYS9XKlnN', '0iCt6L19zvN')
const B = path('0iCYS9XKlnN', '0iCt6L19zvP')
const OTHER_WS = path('0aBcDeFgHiJ', '0iCt6L19zvQ')

/**
 * An in-memory filesystem with the surface the store uses.
 *
 * Real enough to catch a wrong path or a missing pair, and injectable so the
 * rules are tested in plain Node with no temporary directories — matching how
 * the rest of this folder is tested.
 */
function makeFs(initial = {}) {
  const files = new Map(Object.entries(initial))
  const times = new Map()
  let clock = 1000

  const childrenOf = (dir) => {
    const prefix = `${dir}/`
    const names = new Set()
    for (const key of files.keys()) {
      if (!key.startsWith(prefix)) continue
      names.add(key.slice(prefix.length).split('/')[0])
    }
    return [...names]
  }

  return {
    files,
    times,
    async readdir(dir) {
      const names = childrenOf(dir)
      if (names.length === 0) throw new Error('ENOENT')
      return names
    },
    async stat(p) {
      if (!files.has(p)) throw new Error('ENOENT')
      return { size: String(files.get(p)).length, mtimeMs: times.get(p) ?? 0 }
    },
    async readFile(p) {
      if (!files.has(p)) throw new Error('ENOENT')
      return files.get(p)
    },
    async writeFile(p, body) {
      files.set(p, body)
      times.set(p, (clock += 1))
    },
    async mkdir() {},
    async rm(p, { recursive } = {}) {
      if (recursive) {
        for (const key of [...files.keys()]) if (key === p || key.startsWith(`${p}/`)) files.delete(key)
        return
      }
      files.delete(p)
    },
    async utimes(p) {
      times.set(p, (clock += 1))
    },
  }
}

const store = (fs, budget) => createAttachmentStore({ dir: '/cache', fs, budget })
const png = { contentType: 'image/png' }

describe('parseAttachmentPath', () => {
  it('names the workspace and the attachment', () => {
    expect(parseAttachmentPath(A)).toEqual({
      workspaceId: '0iCYS9XKlnN',
      taskId: '0iCYTqxKOqv',
      attachmentId: '0iCt6L19zvN',
    })
  })

  it('is not fooled by a path that merely resembles one', () => {
    expect(parseAttachmentPath('/api/v1/workspaces/ws1/tasks/t1')).toBeNull()
    expect(parseAttachmentPath('/api/v1/workspaces/ws1/tasks/t1/attachments')).toBeNull()
    expect(parseAttachmentPath('')).toBeNull()
    expect(parseAttachmentPath(null)).toBeNull()
  })

  it('refuses an id that must not become a filename', () => {
    // A path built from unvalidated input is how a cache becomes a way to write
    // anywhere on disk. Ids are base62, so anything else is refused outright
    // rather than escaped.
    // Every one of the three becomes a path segment, so every one is checked.
    expect(parseAttachmentPath('/api/v1/workspaces/../../etc/tasks/t1/attachments/a1')).toBeNull()
    expect(parseAttachmentPath('/api/v1/workspaces/ws1/tasks/../../x/attachments/a1')).toBeNull()
    expect(parseAttachmentPath('/api/v1/workspaces/ws1/tasks/t1/attachments/..')).toBeNull()
    expect(parseAttachmentPath('/api/v1/workspaces/ws1/tasks/t1/attachments/a%2E%2E')).toBeNull()
    expect(parseAttachmentPath('/api/v1/workspaces/ws-1/tasks/t1/attachments/a1')).toBeNull()
  })
})

describe('read', () => {
  it('serves a stored attachment with its content type', async () => {
    const fs = makeFs()
    const s = store(fs)
    await s.write(A, 'PNGBYTES', png)

    expect(await s.read(A)).toMatchObject({ body: 'PNGBYTES', contentType: 'image/png' })
  })

  it('mirrors the request path on disk', async () => {
    // The path on disk and the path in the request read the same way, and the
    // attachment id already names that exact file for all time.
    const fs = makeFs()
    await store(fs).write(A, 'BYTES', png)

    expect([...fs.files.keys()]).toEqual([
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN',
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN.meta',
    ])
  })

  it('defaults the disposition when the stored description predates it', async () => {
    // A meta file written by an older build carries no disposition at all,
    // which must read back as empty rather than undefined.
    const fs = makeFs({
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN': 'BYTES',
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN.meta': JSON.stringify({ contentType: 'image/png' }),
    })

    expect((await store(fs).read(A)).contentDisposition).toBe('')
  })

  it('carries a content disposition back, and defaults it when absent', async () => {
    const fs = makeFs()
    const s = store(fs)
    await s.write(A, 'BYTES', { contentType: 'application/pdf', contentDisposition: 'inline' })
    await s.write(B, 'BYTES', png)

    expect((await s.read(A)).contentDisposition).toBe('inline')
    expect((await s.read(B)).contentDisposition).toBe('')
  })

  it('misses for something never stored, or a path it will not touch', async () => {
    const s = store(makeFs())

    expect(await s.read(A)).toBeNull()
    expect(await s.read('/api/v1/workspaces/ws1/tasks/t1')).toBeNull()
  })

  it('misses when the pair is half written or unreadable', async () => {
    // Every one of these means the same thing to the caller: forward to the
    // server, which is what it would have done anyway.
    const noMeta = makeFs({ '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN': 'BYTES' })
    expect(await store(noMeta).read(A)).toBeNull()

    const corrupt = makeFs({
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN': 'BYTES',
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN.meta': 'not json',
    })
    expect(await store(corrupt).read(A)).toBeNull()

    const empty = makeFs({
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN': 'BYTES',
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN.meta': '{}',
    })
    expect(await store(empty).read(A)).toBeNull()
  })

  it('records the hit so eviction knows what is in use', async () => {
    const fs = makeFs()
    const s = store(fs)
    await s.write(A, 'BYTES', png)
    const before = fs.times.get('/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN')

    await s.read(A)

    expect(fs.times.get('/cache/0iCYS9XKlnN/0iCYTqxKOqv/0iCt6L19zvN')).toBeGreaterThan(before)
  })

  it('still serves when the hit cannot be recorded, or cannot be at all', async () => {
    const failing = makeFs()
    await store(failing).write(A, 'BYTES', png)
    failing.utimes = async () => {
      throw new Error('read-only volume')
    }
    expect((await store(failing).read(A)).body).toBe('BYTES')

    const none = makeFs()
    await store(none).write(A, 'BYTES', png)
    delete none.utimes
    expect((await store(none).read(A)).body).toBe('BYTES')
  })
})

describe('write', () => {
  it('refuses a file too large to be worth the space', async () => {
    const fs = makeFs()

    expect(await store(fs).write(A, 'BIG', { ...png, size: 3 * 1024 * 1024 })).toBe(false)
    expect(await store(fs).read(A)).toBeNull()
  })

  it('keeps a response whose size was not measured', async () => {
    // Refusing everything unmeasurable would mean caching almost nothing.
    expect(await store(makeFs()).write(A, 'BYTES', png)).toBe(true)
  })

  it('refuses what it could not serve back, or must not name a file', async () => {
    const fs = makeFs()

    expect(await store(fs).write(A, 'BYTES', {})).toBe(false)
    expect(await store(fs).write('/api/v1/workspaces/ws-1/tasks/t/attachments/a', 'B', png)).toBe(
      false
    )
  })

  it('reports failure rather than throwing when the disk refuses', async () => {
    const fs = makeFs()
    fs.writeFile = async () => {
      throw new Error('disk full')
    }

    expect(await store(fs).write(A, 'BYTES', png)).toBe(false)
  })
})

describe('evict', () => {
  it('drops the least recently used until the budget fits', async () => {
    const fs = makeFs()
    const small = store(fs, 20)
    await small.write(A, '1234567890', png)
    await small.write(B, '1234567890', png)
    await small.write(OTHER_WS, '1234567890', png)

    expect(await small.read(A)).toBeNull()
    expect(await small.read(B)).not.toBeNull()
  })

  it('spares an entry that was read recently', async () => {
    const fs = makeFs()
    const small = store(fs, 20)
    await small.write(A, '1234567890', png)
    await small.write(B, '1234567890', png)
    await small.read(A)

    await small.write(OTHER_WS, '1234567890', png)

    expect(await small.read(A)).not.toBeNull()
    expect(await small.read(B)).toBeNull()
  })

  it('counts across workspaces, since the budget is for the device', async () => {
    const fs = makeFs()
    const small = store(fs, 15)
    await small.write(A, '1234567890', png)

    await small.write(OTHER_WS, '1234567890', png)

    expect(await small.read(A)).toBeNull()
  })

  it('has nothing to drop while the budget fits, or before anything is stored', async () => {
    const fs = makeFs()
    await store(fs).write(A, 'BYTES', png)

    expect(await store(fs).evict()).toBe(0)
    expect(await store(makeFs()).evict()).toBe(0)
  })

  it('ignores names that are not ours, and files that vanish mid-scan', async () => {
    // `.DS_Store` and the like are not base62, so they are never treated as a
    // workspace directory — the scan skips them before touching the disk.
    const fs = makeFs({
      '/cache/.DS_Store': 'x',
      '/cache/tmp-scratch/x': 'y',
      '/cache/0iCYS9XKlnN/0iCYTqxKOqv/not-an-id': 'y',
      '/cache/0iCYS9XKlnN/not-a-task-id/x': 'z',
    })
    await store(fs).write(A, 'BYTES', png)
    const original = fs.stat.bind(fs)
    fs.stat = async (p) => {
      if (p.endsWith('0iCt6L19zvN')) throw new Error('vanished')
      return original(p)
    }

    expect(await store(fs).evict()).toBe(0)
  })

  it('ignores a directory it cannot read at either level', async () => {
    const fs = makeFs()
    await store(fs).write(A, 'BYTES', png)
    const original = fs.readdir.bind(fs)
    fs.readdir = async (dir) => {
      if (dir !== '/cache') throw new Error('EACCES')
      return original(dir)
    }
    expect(await store(fs).evict()).toBe(0)

    const deeper = makeFs()
    await store(deeper).write(A, 'BYTES', png)
    const originalDeeper = deeper.readdir.bind(deeper)
    deeper.readdir = async (dir) => {
      if (dir.split('/').length > 3) throw new Error('EACCES')
      return originalDeeper(dir)
    }
    expect(await store(deeper).evict()).toBe(0)
  })
})

describe('forgetTask', () => {
  it('removes one task and leaves the rest of the workspace', async () => {
    // A directory removal, because the task is a directory — the shape this
    // store was given for exactly this.
    const fs = makeFs()
    const s = store(fs)
    const otherTask = '/api/v1/workspaces/0iCYS9XKlnN/tasks/0iCYVCihMfJ/attachments/0iCt6L19zvR'
    await s.write(A, 'A', png)
    await s.write(B, 'B', png)
    await s.write(otherTask, 'C', png)

    expect(await s.forgetTask('0iCYS9XKlnN', '0iCYTqxKOqv')).toBe(true)

    expect(await s.read(A)).toBeNull()
    expect(await s.read(B)).toBeNull()
    expect(await s.read(otherTask)).not.toBeNull()
  })

  it('does not reach into another workspace holding the same task id', async () => {
    const fs = makeFs()
    const s = store(fs)
    const sameTaskElsewhere = '/api/v1/workspaces/0aBcDeFgHiJ/tasks/0iCYTqxKOqv/attachments/0iCt6L19zvS'
    await s.write(A, 'A', png)
    await s.write(sameTaskElsewhere, 'B', png)

    await s.forgetTask('0iCYS9XKlnN', '0iCYTqxKOqv')

    expect(await s.read(sameTaskElsewhere)).not.toBeNull()
  })

  it('refuses ids that must not become a path', async () => {
    const fs = makeFs()
    await store(fs).write(A, 'A', png)

    expect(await store(fs).forgetTask('../..', 't')).toBe(false)
    expect(await store(fs).forgetTask('0iCYS9XKlnN', '../..')).toBe(false)
    expect(await store(fs).forgetTask('', '0iCYTqxKOqv')).toBe(false)
    expect(await store(fs).forgetTask('0iCYS9XKlnN', '')).toBe(false)
    expect(await store(fs).read(A)).not.toBeNull()
  })

  it('reports failure rather than throwing', async () => {
    const fs = makeFs()
    fs.rm = async () => {
      throw new Error('busy')
    }

    expect(await store(fs).forgetTask('0iCYS9XKlnN', '0iCYTqxKOqv')).toBe(false)
  })
})

describe('forgetWorkspace', () => {
  it('removes one workspace and leaves the others', async () => {
    // One directory removal, because the workspace is a directory. This is what
    // a store we own buys over cache headers.
    const fs = makeFs()
    const s = store(fs)
    await s.write(A, 'A', png)
    await s.write(OTHER_WS, 'B', png)

    expect(await s.forgetWorkspace('0iCYS9XKlnN')).toBe(true)

    expect(await s.read(A)).toBeNull()
    expect(await s.read(OTHER_WS)).not.toBeNull()
  })

  it('refuses a workspace id that must not become a path', async () => {
    const fs = makeFs()
    await store(fs).write(A, 'A', png)

    expect(await store(fs).forgetWorkspace('../..')).toBe(false)
    expect(await store(fs).forgetWorkspace('')).toBe(false)
    expect(await store(fs).read(A)).not.toBeNull()
  })

  it('reports failure rather than throwing', async () => {
    const fs = makeFs()
    fs.rm = async () => {
      throw new Error('busy')
    }

    expect(await store(fs).forgetWorkspace('0iCYS9XKlnN')).toBe(false)
  })
})

describe('forgetAll', () => {
  it('drops everything, which is what signing out needs', async () => {
    const fs = makeFs()
    const s = store(fs)
    await s.write(A, 'A', png)
    await s.write(OTHER_WS, 'B', png)

    expect(await s.forgetAll()).toBe(true)
    expect([...fs.files.keys()]).toEqual([])
  })

  it('reports failure rather than throwing', async () => {
    const fs = makeFs()
    fs.rm = async () => {
      throw new Error('busy')
    }

    expect(await store(fs).forgetAll()).toBe(false)
  })
})

describe('createAttachmentStore', () => {
  it('defaults to the desktop budget, a gigabyte', async () => {
    const fs = makeFs()
    const s = createAttachmentStore({ dir: '/cache', fs })
    await s.write(A, 'BYTES', png)

    expect(await s.evict()).toBe(0)
  })

  it('can be constructed with nothing and simply does nothing', async () => {
    expect(await createAttachmentStore().read(A)).toBeNull()
  })
})
