import { describe, it, expect } from 'vitest'

import {
  MAX_TERMS,
  MIN_TERM_LENGTH,
  STOPWORDS,
  intersect,
  keySignature,
  matchRank,
  rank,
  taskTerms,
  termRanges,
  tokenize,
} from '../src/composables/useTaskIndex'

/**
 * The slice of IDBKeyRange that `termRanges` uses.
 *
 * A stand-in rather than a real implementation, because the range is injected
 * and the only behaviour under test is which bounds get chosen. String keys
 * compare lexicographically in IndexedDB, which is what `>=` and `<=` do here.
 */
const KeyRange = {
  bound: (lower, upper) => ({ lower, upper, includes: (v) => v >= lower && v <= upper }),
}

describe('tokenize', () => {
  it('lowercases and splits on anything that is not a letter or a number', () => {
    expect(tokenize('Ship the billing-reconciliation FIX!')).toEqual([
      'ship',
      'billing',
      'reconciliation',
      'fix',
    ])
  })

  it('keeps non-ASCII words whole', () => {
    // `\w` is ASCII, so a word-character split would cut "café" to "caf" and
    // lose a Turkish or Japanese title outright.
    expect(tokenize('café görüşme 東京')).toEqual(['café', 'görüşme', '東京'])
  })

  it('keeps numbers, which task titles are full of', () => {
    expect(tokenize('Bump to v0.4.18')).toEqual(['bump', 'v0', '18'])
  })

  it('drops the stopwords, which would match most of the workspace', () => {
    expect(tokenize('the fix is in the queue')).toEqual(['fix', 'queue'])
    for (const word of ['the', 'and', 'with']) expect(STOPWORDS.has(word)).toBe(true)
  })

  it('drops single characters', () => {
    expect(tokenize('a b fix c')).toEqual(['fix'])
    expect(MIN_TERM_LENGTH).toBe(2)
  })

  it('dedupes, keeping first appearance', () => {
    expect(tokenize('billing billing refunds billing')).toEqual(['billing', 'refunds'])
  })

  it('caps a runaway body, and truncates from the end', () => {
    // A pasted stack trace would otherwise add thousands of index entries
    // nobody will search for, paid for on every write.
    const many = Array.from({ length: MAX_TERMS + 50 }, (_, i) => `term${i}`).join(' ')
    const terms = tokenize(many)

    expect(terms).toHaveLength(MAX_TERMS)
    expect(terms[0]).toBe('term0')
    expect(terms).not.toContain(`term${MAX_TERMS + 10}`)
  })

  it('has nothing to say about nothing', () => {
    expect(tokenize('')).toEqual([])
    expect(tokenize('   ')).toEqual([])
    expect(tokenize('!!! ... ---')).toEqual([])
    expect(tokenize(null)).toEqual([])
    expect(tokenize(undefined)).toEqual([])
    expect(tokenize(42)).toEqual([])
  })
})

describe('taskTerms', () => {
  it('indexes the title and the body together', () => {
    expect(taskTerms({ title: 'Billing fix', body: 'Refunds double-count' })).toEqual([
      'billing',
      'fix',
      'refunds',
      'double',
      'count',
    ])
  })

  it('puts the title first so the cap costs the body, not the title', () => {
    const task = {
      title: 'Distinctive title here',
      body: Array.from({ length: MAX_TERMS + 50 }, (_, i) => `filler${i}`).join(' '),
    }

    expect(taskTerms(task).slice(0, 3)).toEqual(['distinctive', 'title', 'here'])
  })

  it('copes with a task missing either half', () => {
    expect(taskTerms({ title: 'Only a title' })).toEqual(['only', 'title'])
    expect(taskTerms({ body: 'Only a body' })).toEqual(['only', 'body'])
    expect(taskTerms({})).toEqual([])
    expect(taskTerms(null)).toEqual([])
  })
})

describe('termRanges', () => {
  it('builds one prefix range per token', () => {
    const ranges = termRanges('billing refunds', KeyRange)

    expect(ranges).toHaveLength(2)
    expect(ranges[0].lower).toBe('billing')
    expect(ranges[0].upper).toBe('billing￿')
  })

  it('matches a term the user has only partly typed', () => {
    // This is what saves the index from having to store every prefix of every
    // word: `￿` is the largest code unit, so the bound covers exactly the
    // terms beginning with the token.
    const [range] = termRanges('bill', KeyRange)

    expect(range.includes('bill')).toBe(true)
    expect(range.includes('billing')).toBe(true)
    expect(range.includes('reconciliation')).toBe(false)
  })

  it('constrains nothing when the query cannot match', () => {
    // All stopwords, or nothing at all. Stopwords are never indexed, so a query
    // of them could only ever return empty — the caller shows recents instead.
    expect(termRanges('the and of', KeyRange)).toEqual([])
    expect(termRanges('', KeyRange)).toEqual([])
    expect(termRanges('   ', KeyRange)).toEqual([])
  })
})

describe('keySignature', () => {
  it('makes a compound key comparable', () => {
    // The primary key is [workspaceId, id], and two equal arrays are different
    // objects — comparing by identity would empty every intersection.
    expect(keySignature(['ws1', 't1'])).toBe(keySignature(['ws1', 't1']))
    expect(keySignature(['ws1', 't1'])).not.toBe(keySignature(['ws1', 't2']))
  })

  it('accepts a plain key too', () => {
    expect(keySignature('ws1')).toBe('ws1')
  })
})

describe('intersect', () => {
  const k = (id) => ['ws1', id]

  it('keeps only the keys every list holds', () => {
    // AND semantics: a second word narrows the result rather than widening it.
    const result = intersect([
      [k('a'), k('b'), k('c')],
      [k('b'), k('c'), k('d')],
      [k('c'), k('b')],
    ])

    expect(result.map((key) => key[1])).toEqual(['b', 'c'])
  })

  it('keeps the first list in its incoming order', () => {
    const result = intersect([
      [k('c'), k('a'), k('b')],
      [k('a'), k('b'), k('c')],
    ])

    expect(result.map((key) => key[1])).toEqual(['c', 'a', 'b'])
  })

  it('returns the first list unchanged when there is only one', () => {
    expect(intersect([[k('a'), k('b')]]).map((key) => key[1])).toEqual(['a', 'b'])
  })

  it('returns nothing when one list matches nothing', () => {
    expect(intersect([[k('a')], []])).toEqual([])
    expect(intersect([[], [k('a')]])).toEqual([])
  })

  it('has nothing to intersect when nothing was asked', () => {
    expect(intersect([])).toEqual([])
    expect(intersect(null)).toEqual([])
    expect(intersect(undefined)).toEqual([])
  })

  // A prefix seek over a multiEntry index returns one key per matching *term*,
  // so a task indexed under both "task" and "tasks" comes back twice for the
  // query "task". Undeduplicated it is read twice and drawn twice.
  it('returns a key once however many times a list repeats it', () => {
    const result = intersect([[k('a'), k('b'), k('a'), k('a')]])

    expect(result.map((key) => key[1])).toEqual(['a', 'b'])
  })

  it('keeps the first occurrence, so the incoming order still holds', () => {
    const result = intersect([[k('c'), k('a'), k('c'), k('b')]])

    expect(result.map((key) => key[1])).toEqual(['c', 'a', 'b'])
  })

  it('deduplicates a key that survives the intersection', () => {
    // The repeat has to be dropped *and* the AND semantics kept: "a" appears
    // twice on the left and once on the right, "b" only on the left.
    const result = intersect([
      [k('a'), k('b'), k('a')],
      [k('a')],
    ])

    expect(result.map((key) => key[1])).toEqual(['a'])
  })

  it('drops a repeated key that no other list holds', () => {
    expect(intersect([[k('a'), k('a')], [k('b')]])).toEqual([])
  })
})

describe('matchRank', () => {
  const task = { id: '0iCYTqxKOqv', title: 'Ship the billing fix', body: 'Refunds double-count' }

  it('ranks an exact ID above everything', () => {
    expect(matchRank(task, '0iCYTqxKOqv')).toBe(0)
    expect(matchRank(task, '0icytqxkoqv')).toBe(0)
  })

  it('then an ID prefix, then a title match, then a body match', () => {
    expect(matchRank(task, '0iCYT')).toBe(1)
    expect(matchRank(task, 'billing')).toBe(2)
    expect(matchRank(task, 'refunds')).toBe(3)
  })

  it('reports no match for text that appears nowhere', () => {
    expect(matchRank(task, 'quarterly')).toBe(-1)
  })

  it('reports no match for an empty query', () => {
    expect(matchRank(task, '')).toBe(-1)
    expect(matchRank(task, '   ')).toBe(-1)
    expect(matchRank(task, null)).toBe(-1)
  })

  it('copes with a task missing the fields it reads', () => {
    expect(matchRank({}, 'billing')).toBe(-1)
    expect(matchRank(null, 'billing')).toBe(-1)
  })
})

describe('rank', () => {
  const tasks = [
    { id: 'aaa', title: 'Mentions refunds in the body only', body: 'billing' },
    { id: 'bbb', title: 'Billing dashboard copy', body: '' },
    { id: 'billingxxxx', title: 'Named like the query', body: '' },
  ]

  it('orders ID over title over body', () => {
    expect(rank(tasks, 'billing').map((t) => t.id)).toEqual(['billingxxxx', 'bbb', 'aaa'])
  })

  it('keeps a task the index found even when the text does not contain the query verbatim', () => {
    // The index matches whole terms with a prefix, so "reconcile" legitimately
    // finds a body saying "reconciliation". Dropping it would throw away a
    // correct hit — it just ranks behind the literal matches.
    const found = [
      { id: 'x', title: 'Reconciliation run', body: '' },
      { id: 'y', title: 'Exact reconcile match', body: '' },
    ]

    expect(rank(found, 'reconcile').map((t) => t.id)).toEqual(['y', 'x'])
  })

  it('keeps two index-only hits in their incoming order behind the literal ones', () => {
    // Both of these were returned by the term index — "reconciliation" and
    // "reconciling" both start with "reconcil" — but neither contains the
    // query verbatim, so they tie and fall back to the order they arrived in.
    const found = [
      { id: 'literal', title: 'reconcile now', body: '' },
      { id: 'first', title: 'Reconciliation run', body: '' },
      { id: 'second', title: 'Reconciling nightly', body: '' },
    ]

    expect(rank(found, 'reconcile').map((t) => t.id)).toEqual(['literal', 'first', 'second'])
  })

  it('keeps incoming order for ties, which is newest first', () => {
    const tied = [
      { id: '1', title: 'billing one' },
      { id: '2', title: 'billing two' },
      { id: '3', title: 'billing three' },
    ]

    expect(rank(tied, 'billing').map((t) => t.id)).toEqual(['1', '2', '3'])
  })

  it('caps the list so the panel stays a panel', () => {
    const many = Array.from({ length: 20 }, (_, i) => ({ id: `${i}`, title: 'billing' }))

    expect(rank(many, 'billing')).toHaveLength(8)
    expect(rank(many, 'billing', 3)).toHaveLength(3)
  })

  it('copes with a list that never arrived', () => {
    expect(rank(undefined, 'billing')).toEqual([])
    expect(rank(null, 'billing')).toEqual([])
    expect(rank([], 'billing')).toEqual([])
  })
})
