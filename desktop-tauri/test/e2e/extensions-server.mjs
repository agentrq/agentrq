// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The server for extensions.js: prints each step it is sent, one JSON line
// each, and leaves on `done` or `failed`. Everything else is a 404.
//
//     node test/e2e/extensions-server.mjs <port>
import { createServer } from 'node:http'

const server = createServer((request, response) => {
  if (request.method !== 'POST' || request.url !== '/api/v1/__acceptance') {
    response.writeHead(404).end()
    return
  }
  let body = ''
  request.on('data', (chunk) => (body += chunk))
  request.on('end', () => {
    response.writeHead(204).end()
    console.log(body)
    const { step } = JSON.parse(body)
    if (step === 'done' || step === 'failed') server.close(() => process.exit(step === 'done' ? 0 : 1))
  })
})
server.listen(Number(process.argv[2]), '127.0.0.1')
