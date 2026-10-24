// S9: launches an app, waits, and sums the memory of its whole process tree
// (on macOS also WebKit's XPC processes, which launchd parents).
//   node scripts/measure.mjs <label> <seconds> <out.json> -- <cmd> [args...]
// Never merge.
import { spawn, execFileSync } from 'node:child_process'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'

const [label, seconds, outFile, dashdash, cmd, ...args] = process.argv.slice(2)
if (dashdash !== '--') throw new Error('usage')
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function table() {
  if (process.platform === 'linux') {
    const rows = []
    for (const d of readdirSync('/proc')) {
      if (!/^\d+$/.test(d)) continue
      try {
        const stat = readFileSync(`/proc/${d}/stat`, 'utf8')
        const after = stat.slice(stat.lastIndexOf(')') + 2).split(' ')
        const comm = stat.slice(stat.indexOf('(') + 1, stat.lastIndexOf(')'))
        const status = readFileSync(`/proc/${d}/status`, 'utf8')
        const rss = Number(status.match(/VmRSS:\s+(\d+)/)?.[1] || 0)
        let pss = 0
        try { pss = Number(readFileSync(`/proc/${d}/smaps_rollup`, 'utf8').match(/Pss:\s+(\d+)/)?.[1] || 0) } catch {}
        rows.push({ pid: Number(d), ppid: Number(after[1]), comm, rssKb: rss, pssKb: pss })
      } catch {}
    }
    return rows
  }
  if (process.platform === 'darwin') {
    return execFileSync('ps', ['-axo', 'pid=,ppid=,rss=,comm='], { encoding: 'utf8' }).trim().split('\n').map((l) => {
      const m = l.trim().match(/^(\d+)\s+(\d+)\s+(\d+)\s+(.*)$/)
      return { pid: +m[1], ppid: +m[2], rssKb: +m[3], comm: m[4] }
    })
  }
  const csv = execFileSync('powershell', ['-NoProfile', '-Command',
    'Get-CimInstance Win32_Process | ForEach-Object { "$($_.ProcessId),$($_.ParentProcessId),$($_.WorkingSetSize),$($_.PrivatePageCount),$($_.Name)" }'], { encoding: 'utf8' })
  return csv.trim().split(/\r?\n/).map((l) => {
    const [pid, ppid, ws, priv, ...name] = l.split(',')
    return { pid: +pid, ppid: +ppid, rssKb: Math.round(+ws / 1024), privateKb: Math.round(+priv / 1024), comm: name.join(',') }
  })
}

const child = spawn(cmd, args, { stdio: 'ignore', env: process.env })
await sleep(Number(seconds) * 1000)
const rows = table()
const tree = new Set([child.pid])
for (let grew = true; grew;) {
  grew = false
  for (const r of rows) if (!tree.has(r.pid) && tree.has(r.ppid)) { tree.add(r.pid); grew = true }
}
const procs = rows.filter((r) => tree.has(r.pid) || (process.platform === 'darwin' && /com\.apple\.WebKit/.test(r.comm)))
const sum = (k) => procs.reduce((a, r) => a + (r[k] || 0), 0)
const result = {
  label, os: process.platform, seconds: Number(seconds), alive: child.exitCode === null,
  processes: procs.map((r) => ({ comm: r.comm.split(/[\\/]/).pop(), rssMb: +(r.rssKb / 1024).toFixed(1) })),
  totalRssMb: +(sum('rssKb') / 1024).toFixed(1),
  ...(process.platform === 'linux' ? { totalPssMb: +(sum('pssKb') / 1024).toFixed(1) } : {}),
  ...(process.platform === 'win32' ? { totalPrivateMb: +(sum('privateKb') / 1024).toFixed(1) } : {}),
}
writeFileSync(outFile, JSON.stringify(result, null, 2))
console.log(JSON.stringify(result))
// Take the whole tree down.
if (process.platform === 'win32') { try { execFileSync('taskkill', ['/PID', String(child.pid), '/T', '/F']) } catch {} }
else { for (const r of procs) { try { process.kill(r.pid, 'SIGKILL') } catch {} } }
process.exit(0)
