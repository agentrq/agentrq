/**
 * Development launcher.
 *
 * The renderer is served by a Vite dev server, but Electron still loads the app
 * from app:// — the protocol handler proxies static requests to Vite instead of
 * reading from disk. That keeps dev and production on the identical origin, so
 * cookies, routing and the API proxy behave the same in both, while HMR still
 * works.
 *
 * Main and preload are rebuilt on change; Electron is restarted when they
 * change, because there is no hot reload for the main process.
 */
import { build, createServer } from 'vite'
import { spawn } from 'node:child_process'
import { fileURLToPath, URL } from 'node:url'
import electronPath from 'electron'

const config = (name) => fileURLToPath(new URL(`../vite.${name}.config.mjs`, import.meta.url))
const root = fileURLToPath(new URL('..', import.meta.url))

let child = null
let restarting = false

function startElectron(devServerUrl) {
  child = spawn(electronPath, ['.'], {
    cwd: root,
    stdio: 'inherit',
    env: { ...process.env, AGENTRQ_RENDERER_DEV_URL: devServerUrl, NODE_ENV: 'development' },
  })
  child.on('close', (code) => {
    // A restart kills the child on purpose; only a real exit should end the
    // dev session.
    if (!restarting) process.exit(code ?? 0)
  })
}

async function restartElectron(devServerUrl) {
  if (!child) return startElectron(devServerUrl)
  restarting = true
  child.kill()
  await new Promise((resolve) => child.once('close', resolve))
  restarting = false
  startElectron(devServerUrl)
}

const server = await createServer({ configFile: config('renderer') })
await server.listen()
const devServerUrl = server.resolvedUrls.local[0].replace(/\/$/, '')
console.log(`▸ renderer dev server on ${devServerUrl}`)

let booted = false
for (const target of ['main', 'preload']) {
  await build({
    configFile: config(target),
    build: { watch: {} },
    plugins: [
      {
        name: 'agentrq-restart-electron',
        closeBundle() {
          if (booted) restartElectron(devServerUrl)
        },
      },
    ],
  })
}

booted = true
startElectron(devServerUrl)

const shutdown = async () => {
  restarting = true
  child?.kill()
  await server.close()
  process.exit(0)
}
process.on('SIGINT', shutdown)
process.on('SIGTERM', shutdown)
