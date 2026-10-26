// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The server for extensions.js: prints each step it is sent, one JSON line
// each, and leaves on `done` or `failed`. It also plays the AgentRQ server
// where the steps need one:
//
// - the event stream, which carries a tool call waiting on a person once the
//   page says the guardrail is installed; the verdict the app sends back is
//   kept for the page to read;
// - the supervisor's OAuth endpoints, whose authorise page answers with a real
//   redirect to the app's own scheme, as the server's does.
//
// Everything else is a 404.
//
//     node test/e2e/extensions-server.mjs <port>
import { createServer } from 'node:http'

const REDIRECT_URI = 'agentrq://extensions/authorized'

/** A tool call waiting on a person, as the server's stream carries one. */
const PENDING = {
  type: 'reply.received',
  payload: {
    id: 't1',
    workspaceId: 'ws1',
    messages: [
      {
        metadata: {
          type: 'permission_request',
          status: 'pending',
          requestId: 'r1',
          toolName: 'Bash',
          inputPreview: JSON.stringify({ command: 'rm -rf /tmp/build' }),
        },
      },
    ],
  },
}

const streams = new Set()
let guarding = false
let verdict = null

const print = (step, value) => console.log(JSON.stringify({ step, value }))
const sendJson = (response, status, body) =>
  response.writeHead(status, { 'content-type': 'application/json' }).end(JSON.stringify(body))

function readBody(request) {
  return new Promise((resolve) => {
    let body = ''
    request.on('data', (chunk) => (body += chunk))
    request.on('end', () => resolve(body))
  })
}

function tellPending(stream) {
  stream.write(`data: ${JSON.stringify(PENDING)}\n\n`)
}

const server = createServer(async (request, response) => {
  const url = new URL(request.url, 'http://127.0.0.1')
  const route = `${request.method} ${url.pathname}`

  if (route === 'GET /api/v1/events/stream') {
    response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-store' })
    response.write(': open\n\n')
    streams.add(response)
    request.on('close', () => streams.delete(response))
    if (guarding) tellPending(response)
    return
  }
  if (route === 'POST /api/v1/workspaces/ws1/tasks/t1/permission') {
    verdict = JSON.parse(await readBody(request))
    print('verdict sent to the server', verdict)
    sendJson(response, 200, {})
    return
  }
  if (route === 'GET /api/v1/__acceptance/verdict') {
    sendJson(response, 200, verdict)
    return
  }
  if (route === 'POST /mcp/oauth2/register') {
    sendJson(response, 201, { client_id: 'acceptance-client' })
    return
  }
  if (route === 'GET /mcp/oauth2/authorize') {
    const asked = Object.fromEntries(url.searchParams)
    print('authorise page asked', { clientId: asked.client_id, redirectUri: asked.redirect_uri })
    if (asked.redirect_uri !== REDIRECT_URI) {
      response.writeHead(400).end()
      return
    }
    const back = new URL(REDIRECT_URI)
    back.searchParams.set('code', 'acceptance-code')
    back.searchParams.set('state', asked.state ?? '')
    response.writeHead(302, { location: back.href }).end()
    return
  }
  if (route === 'POST /mcp/oauth2/token') {
    const form = new URLSearchParams(await readBody(request))
    print('token asked', { grantType: form.get('grant_type'), code: form.get('code') })
    sendJson(response, 200, { access_token: 'access-1', refresh_token: 'refresh-1', token_type: 'Bearer', expires_in: 3600 })
    return
  }
  if (route !== 'POST /api/v1/__acceptance') {
    response.writeHead(404).end()
    return
  }
  const body = await readBody(request)
  response.writeHead(204).end()
  console.log(body)
  const { step } = JSON.parse(body)
  if (step === 'guardrail ready') {
    guarding = true
    for (const stream of streams) tellPending(stream)
  }
  if (step === 'done' || step === 'failed') {
    for (const stream of streams) stream.end()
    server.close(() => process.exit(step === 'done' ? 0 : 1))
  }
})
server.listen(Number(process.argv[2]), '127.0.0.1')
