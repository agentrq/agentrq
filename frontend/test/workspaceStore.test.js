import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

const fetchWorkspaces = vi.fn()
vi.mock('../src/api', () => ({ fetchWorkspaces: (...args) => fetchWorkspaces(...args) }))

const { useWorkspaceStore } = await import('../src/stores/workspaceStore')

const ws = (id, name, extra = {}) => ({ id, name, agentConnected: false, ...extra })

describe('workspaceStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    fetchWorkspaces.mockReset()
  })

  describe('fetchWorkspaces', () => {
    it('sorts the list by name, so the sidebar order never depends on the server', async () => {
      fetchWorkspaces.mockResolvedValue({ workspaces: [ws('b', 'zeta'), ws('a', 'alpha')] })
      const store = useWorkspaceStore()

      await store.fetchWorkspaces()

      expect(store.workspaces.map(w => w.name)).toEqual(['alpha', 'zeta'])
      expect(store.loading).toBe(false)
    })

    it('sorts numerically, so agent-10 comes after agent-2', async () => {
      fetchWorkspaces.mockResolvedValue({ workspaces: [ws('a', 'agent-10'), ws('b', 'agent-2')] })
      const store = useWorkspaceStore()

      await store.fetchWorkspaces()

      expect(store.workspaces.map(w => w.name)).toEqual(['agent-2', 'agent-10'])
    })

    it('treats a response with no list as an empty one', async () => {
      fetchWorkspaces.mockResolvedValue({})
      const store = useWorkspaceStore()

      await store.fetchWorkspaces()

      expect(store.workspaces).toEqual([])
    })

    it('keeps what it had when the request fails, and stops loading', async () => {
      fetchWorkspaces.mockResolvedValueOnce({ workspaces: [ws('a', 'alpha')] })
      const store = useWorkspaceStore()
      await store.fetchWorkspaces()

      const err = vi.spyOn(console, 'error').mockImplementation(() => {})
      fetchWorkspaces.mockRejectedValueOnce(new Error('offline'))
      await store.fetchWorkspaces()
      err.mockRestore()

      expect(store.workspaces.map(w => w.id)).toEqual(['a'])
      expect(store.loading).toBe(false)
    })
  })

  describe('updateAgentStatus', () => {
    beforeEach(async () => {
      fetchWorkspaces.mockResolvedValue({ workspaces: [ws('0ZzhYQG2qtl', 'alpha'), ws('0iAx25vra8v', 'beta')] })
      await useWorkspaceStore().fetchWorkspaces()
    })

    it('marks the named workspace connected and leaves the others alone', () => {
      const store = useWorkspaceStore()

      store.updateAgentStatus('0ZzhYQG2qtl', true)

      expect(store.isAgentConnected('0ZzhYQG2qtl')).toBe(true)
      expect(store.isAgentConnected('0iAx25vra8v')).toBe(false)
    })

    it('marks it disconnected again', () => {
      const store = useWorkspaceStore()
      store.updateAgentStatus('0ZzhYQG2qtl', true)

      store.updateAgentStatus('0ZzhYQG2qtl', false)

      expect(store.isAgentConnected('0ZzhYQG2qtl')).toBe(false)
    })

    // The bug this store exists to prevent: the backend used to publish the
    // workspace ID as a raw int64 while the API names workspaces in base62, so
    // no lookup ever matched and the indicator never moved.
    it('ignores an ID that names no workspace it holds', () => {
      const store = useWorkspaceStore()

      store.updateAgentStatus(1234567890123, true)

      expect(store.workspaces.some(w => w.agentConnected)).toBe(false)
    })

    it('matches an ID that arrives as a number rather than a string', () => {
      fetchWorkspaces.mockResolvedValue({ workspaces: [ws(42, 'numeric')] })
      const store = useWorkspaceStore()
      store.workspaces = [ws(42, 'numeric')]

      store.updateAgentStatus('42', true)

      expect(store.isAgentConnected(42)).toBe(true)
    })

    it('ignores a missing ID rather than matching the first row', () => {
      const store = useWorkspaceStore()

      store.updateAgentStatus(undefined, true)
      store.updateAgentStatus(null, true)

      expect(store.workspaces.some(w => w.agentConnected)).toBe(false)
    })
  })

  describe('updateWorkspaceMetadata', () => {
    beforeEach(async () => {
      fetchWorkspaces.mockResolvedValue({ workspaces: [ws('a', 'alpha'), ws('b', 'beta')] })
      await useWorkspaceStore().fetchWorkspaces()
    })

    it('merges the new fields over the old ones', () => {
      const store = useWorkspaceStore()

      store.updateWorkspaceMetadata({ id: 'a', description: 'now described' })

      expect(store.getWorkspace('a')).toMatchObject({ name: 'alpha', description: 'now described' })
    })

    it('re-sorts, because a rename can change the order', () => {
      const store = useWorkspaceStore()

      store.updateWorkspaceMetadata({ id: 'a', name: 'zulu' })

      expect(store.workspaces.map(w => w.name)).toEqual(['beta', 'zulu'])
    })

    it('ignores a workspace it does not hold', () => {
      const store = useWorkspaceStore()

      store.updateWorkspaceMetadata({ id: 'never-seen', name: 'ghost' })

      expect(store.workspaces).toHaveLength(2)
    })

    it('ignores an update with no workspace in it', () => {
      const store = useWorkspaceStore()

      store.updateWorkspaceMetadata(undefined)

      expect(store.workspaces).toHaveLength(2)
    })
  })

  describe('reads', () => {
    it('returns undefined for a workspace it does not hold', () => {
      expect(useWorkspaceStore().getWorkspace('nope')).toBeUndefined()
    })

    // False, not undefined: every caller renders this as a boolean, and an
    // unknown workspace has to read the same as an offline one.
    it('reports an unknown workspace as offline', () => {
      expect(useWorkspaceStore().isAgentConnected('nope')).toBe(false)
    })

    it('reports a workspace with no connection field yet as offline', () => {
      const store = useWorkspaceStore()
      store.workspaces = [{ id: 'a', name: 'alpha' }]

      expect(store.isAgentConnected('a')).toBe(false)
    })
  })
})
