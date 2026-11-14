// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { execFileSync } from 'node:child_process'
import { createHmac, randomInt } from 'node:crypto'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { dataDir, jwtSecret } from './settings.js'

// A second person, for what takes two (teams). The run's server signs people
// in only as root, or through Google and GitHub, which a run cannot reach. So
// the person is a row in the run's own database, signed in with a session the
// server itself would have minted: its claims, signed with its secret. The
// server, the page and every call after are the real ones.

const BASE62 = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'

// An id as the server writes one: base62, padded to 11 characters. The live
// stream is keyed by that string, so an unpadded one would hear nothing.
function base62(n) {
  let s = ''
  for (let v = n; v > 0n; v /= 62n) s = BASE62[Number(v % 62n)] + s
  return s.padStart(11, '0')
}

const b64url = (v) => Buffer.from(typeof v === 'string' ? v : JSON.stringify(v)).toString('base64url')

function sessionToken(userId, email, name) {
  const head = b64url({ alg: 'HS256', typ: 'JWT' })
  const body = b64url({ sub: userId, aud: ['actor:human'], exp: Math.floor(Date.now() / 1000) + 3600, email, name })
  const sig = createHmac('sha256', jwtSecret).update(`${head}.${body}`).digest('base64url')
  return `${head}.${body}.${sig}`
}

// On Postgres (QA_POSTGRES_HOST) the row goes in through psql.
function pgUserId(email, name) {
  const psql = (sql, vars) => execFileSync('psql', ['-h', process.env.QA_POSTGRES_HOST, '-U', 'postgres', '-d', 'agentrq', '-tAq', '-v', 'ON_ERROR_STOP=1',
    ...Object.entries(vars).flatMap(([k, v]) => ['-v', `${k}=${v}`])], { input: sql, env: { ...process.env, PGPASSWORD: 'postgres' } }).toString().trim()
  const id = (BigInt(Date.now()) << 22n) | BigInt(randomInt(1 << 22))
  return BigInt(psql(`INSERT INTO users (id, created_at, updated_at, email, name, picture) VALUES (:'id', now(), now(), :'email', :'name', '')
    ON CONFLICT DO NOTHING; SELECT id FROM users WHERE email = :'email';`, { id, email, name }))
}

/** Creates the person (once per email) and signs `context` in as them. */
export async function signInAs(context, { email, name }) {
  if (process.env.QA_POSTGRES_HOST) {
    const userId = base62(pgUserId(email, name))
    await context.addCookies([{ name: 'at', value: sessionToken(userId, email, name), domain: 'localhost', path: '/' }])
    return { userId, email, name }
  }
  const db = new DatabaseSync(join(dataDir, 'agentrq.db'))
  try {
    db.exec('PRAGMA busy_timeout = 5000')
    // Ids are int64s past a JS number: read and written as BigInts.
    const find = db.prepare('SELECT id FROM users WHERE email = ?')
    find.setReadBigInts(true)
    let row = find.get(email)
    if (!row) {
      const id = (BigInt(Date.now()) << 22n) | BigInt(randomInt(1 << 22))
      const now = new Date().toISOString().replace('T', ' ').replace('Z', '+00:00')
      const insert = db.prepare('INSERT INTO users (id, created_at, updated_at, email, name, picture) VALUES (?, ?, ?, ?, ?, ?)')
      insert.setReadBigInts(true)
      insert.run(id, now, now, email, name, '')
      row = { id }
    }
    const userId = base62(BigInt(row.id))
    await context.addCookies([{ name: 'at', value: sessionToken(userId, email, name), domain: 'localhost', path: '/' }])
    return { userId, email, name }
  } finally {
    db.close()
  }
}
