// Runs the WP0 spikes against the stand-in server and collects their JSON.
//   node scripts/run-spike.mjs <binary> [spike ...]
// Linux: run it under `xvfb-run -a dbus-run-session --`. Never merge.
import { spawn, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync, unlinkSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import os from 'node:os'

const here = dirname(fileURLToPath(import.meta.url))
const [bin, ...wanted] = process.argv.slice(2)
const OS = process.platform
const out = resolve(process.env.SPIKE_OUT || join(here, '..', 'spike-out'))
const shots = join(out, 'shots')
mkdirSync(shots, { recursive: true })
const PORT = 3917
const SERVER = `http://127.0.0.1:${PORT}`
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const server = spawn(process.execPath, [join(here, '..', 'testserver', 'server.mjs')], { env: { ...process.env, PORT: String(PORT) }, stdio: 'inherit' })
await sleep(800)

// The screen-grab helper the app asks through shots/<name>.req.
function grab(file) {
  if (OS === 'linux') return spawnSync('import', ['-window', 'root', file]).status === 0
  if (OS === 'darwin') return spawnSync('screencapture', ['-x', file]).status === 0
  const ps = `Add-Type -AssemblyName System.Windows.Forms,System.Drawing;$b=[System.Windows.Forms.SystemInformation]::VirtualScreen;$i=New-Object System.Drawing.Bitmap $b.Width,$b.Height;$g=[System.Drawing.Graphics]::FromImage($i);$g.CopyFromScreen($b.Left,$b.Top,0,0,$i.Size);$i.Save('${file}')`
  return spawnSync('powershell', ['-NoProfile', '-Command', ps]).status === 0
}
let grabbing = true
;(async () => {
  while (grabbing) {
    for (const f of readdirSync(shots)) {
      if (!f.endsWith('.req')) continue
      const name = f.slice(0, -4)
      unlinkSync(join(shots, f))
      const ok = grab(join(shots, `${OS}-${name}.png`))
      writeFileSync(join(shots, `${name}.png`), ok ? 'ok' : 'failed')
    }
    await sleep(100)
  }
})()

function run(spike, env = {}, timeoutS = 60) {
  const file = join(out, `${spike}${env.TAG ? '-' + env.TAG : ''}.json`)
  rmSync(file, { force: true })
  return new Promise((done) => {
    const t0 = Date.now()
    const child = spawn(bin, [], {
      env: { ...process.env, SPIKE: spike, SERVER, OUT: file, SPIKE_TIMEOUT: String(timeoutS), ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let log = ''
    child.stdout.on('data', (d) => { log += d })
    child.stderr.on('data', (d) => { log += d; for (const l of String(d).split('\n')) if (l.includes('[spike]')) console.log(l) })
    const kill = setTimeout(() => child.kill('SIGKILL'), (timeoutS + 20) * 1000)
    child.on('exit', (code, signal) => {
      clearTimeout(kill)
      writeFileSync(file.replace(/\.json$/, '.log'), log)
      let result = null
      try { result = JSON.parse(readFileSync(file, 'utf8')) } catch {}
      console.log(`== ${spike}${env.TAG ? ' ' + env.TAG : ''}: exit ${code ?? signal} in ${Date.now() - t0} ms`)
      done({ spike, tag: env.TAG || '', exit: code ?? signal, result })
    })
  })
}

const all = []
const want = (s) => wanted.length === 0 || wanted.includes(s)
const data = join(os.tmpdir(), `agentrq-tauri-spike-${Date.now()}`)

if (want('bodies')) {
  // Each body kind in its own process: a crash must name its culprit.
  for (const k of ['string', 'bytes5m', 'blob', 'formSmall', 'formFile']) all.push(await run('bodies', { Q: `only=${k}`, TAG: k }, 30))
}
if (want('s2')) all.push(await run('s2', { SPIKE_DATA: data }, 60))
if (want('s4')) all.push(await run('s4', { SPIKE_DATA: join(data, 's4') }, 45))
if (want('s5')) {
  all.push(await run('s5a', { SPIKE_DATA: join(data, 's5'), TAG: 'write' }, 45))
  all.push(await run('s5b', { SPIKE_DATA: join(data, 's5'), TAG: 'read' }, 45))
  if (OS === 'darwin') {
    // macOS < 14 has no data store identifier: does a per-profile host isolate instead?
    all.push(await run('s5a', { SPIKE_DATA: join(data, 's5h'), S5_HOST: '1', TAG: 'host-write' }, 45))
    all.push(await run('s5b', { SPIKE_DATA: join(data, 's5h'), S5_HOST: '1', TAG: 'host-read' }, 45))
  }
}
if (want('s6')) all.push(await run('s6', { SPIKE_DATA: join(data, 's6') }, 60))
if (want('s7')) {
  let fake = null
  if (OS === 'linux') {
    // A notification server that "clicks" every notification after a second.
    fake = spawn('python3', [join(here, 'fake-notifyd.py')], { stdio: 'inherit' })
    await sleep(1500)
  }
  all.push(await run('s7', { SPIKE_DATA: join(data, 's7') }, OS === 'linux' ? 30 : 15))
  fake?.kill()
}
if (want('s8') && OS === 'darwin') {
  const p = run('s8', { SPIKE_DATA: join(data, 's8') }, 90)
  const targetsFile = join(out, 's8-targets.json')
  for (let i = 0; i < 100 && !existsSync(targetsFile); i++) await sleep(200)
  const steps = []
  if (existsSync(targetsFile)) {
    const t = JSON.parse(readFileSync(targetsFile, 'utf8'))
    const pos = () => { try { return JSON.parse(readFileSync(join(out, 's8-pos.json'), 'utf8')) } catch { return null } }
    const swift = (...a) => spawnSync('swift', [join(here, 'macmouse.swift'), ...a.map(String)], { encoding: 'utf8' })
    for (const [name, kind] of [['aBtn', 'click'], ['bBtn', 'click'], ['aEmpty', 'drag'], ['aTitle', 'drag'], ['bTitle', 'drag'], ['bNoDrag', 'drag']]) {
      await sleep(600)
      const before = pos()
      const c = t[name]
      const r = kind === 'click' ? swift('click', c.x, c.y) : swift('drag', c.x, c.y, c.x + 60, c.y + 40)
      await sleep(900)
      const after = pos()
      const moved = !!after && JSON.stringify(after) !== JSON.stringify(before)
      steps.push({ name, kind, at: c, before, after, moved, swift: { status: r.status, err: r.stderr?.slice(0, 300) } })
      console.log(`s8 ${name} ${kind}: moved=${moved}`)
      // Put the window back so every target stays where we measured it.
      if (moved && before) swift('drag', c.x + 60, c.y + 40, c.x, c.y)
      await sleep(600)
    }
  }
  writeFileSync(join(out, 's8-done'), '')
  const r = await p
  r.result = { ...(r.result || {}), steps }
  all.push(r)
}

grabbing = false
server.kill()
writeFileSync(join(out, `summary-${OS}.json`), JSON.stringify(all, null, 2))
console.log(JSON.stringify(all.map((r) => ({ spike: r.spike, tag: r.tag, exit: r.exit })), null, 0))
process.exit(0)
