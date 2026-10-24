// WP0 S6: the side panel as a child webview. Never merge.
import { T, report, finish, sleep, step } from '/common.js'
const host = document.getElementById('host')
const dialog = document.getElementById('dialog')
const toast = document.getElementById('toast')
const rectOf = (el) => { const r = el.getBoundingClientRect(); return { x: r.left, y: r.top, w: r.width, h: r.height } }
const panelReport = async (tag) => {
  for (let i = 0; i < 40; i++) {
    const v = await (await fetch('/api/v1/__report/panel-' + tag)).json()
    if (v) return v
    await sleep(150)
  }
  return null
}

// `at` in the profile's own store, so a leak into the guest would show.
await step('s6-profile-cookie', () => T.invoke('set_profile_cookie'))

const r0 = rectOf(host)
await step('s6-open', async () => { await T.invoke('panel_open', { rect: r0 }); return { rect: r0 } })
const load = await panelReport('load')
await report('s6-placement', {
  expected: r0, guest: load, info: await T.invoke('panel_info'),
  pass: !!load && Math.abs(load.innerWidth - r0.w) <= 2 && Math.abs(load.innerHeight - r0.h) <= 2,
})
await report('s6-isolation', {
  guestSawAt: load?.who?.hasAt, guestCookie: load?.cookie,
  pass: !!load && load.who.hasAt === false && !load.cookie.includes('at='),
})

// Resize from a new DOM rect, as the ResizeObserver would.
host.style.left = '800px'; host.style.width = '400px'
const r1 = rectOf(host)
const boundsMs = await T.invoke('panel_bounds', { rect: r1 })
const resized = await panelReport('resize1')
await report('s6-resize', { expected: r1, guest: resized, setBoundsMs: boundsMs, info: await T.invoke('panel_info'),
  pass: !!resized && Math.abs(resized.innerWidth - r1.w) <= 2 })

// Overlays: does the app's dialog and toast draw over the guest?
dialog.style.display = 'flex'; toast.style.display = 'flex'
await sleep(300)
await report('s6-shot-native', await T.invoke('shot', { name: 's6-1-native-overlay' }))

// Mitigation (a): hide the guest and show its snapshot in the host.
await step('s6-mitigation-a', async () => {
  const t0 = performance.now()
  const buf = await T.invoke('panel_snapshot')
  const tSnap = performance.now() - t0
  const img = new Image()
  img.src = URL.createObjectURL(new Blob([buf], { type: 'image/png' }))
  await img.decode()
  host.replaceChildren(img)
  const hideMs = await T.invoke('panel_visible', { visible: false })
  const total = performance.now() - t0
  await sleep(300)
  const shotOk = await T.invoke('shot', { name: 's6-2-snapshot-overlay' })
  return { snapshotRoundTripMs: Math.round(tSnap), hideMs, totalMs: Math.round(total), pngBytes: buf.byteLength, img: { w: img.naturalWidth, h: img.naturalHeight }, shotOk, pass: buf.byteLength > 1000 }
})
await T.invoke('panel_visible', { visible: true })
host.replaceChildren()

// Mitigation (b): shrink the guest out of the dialog's rect.
await step('s6-mitigation-b', async () => {
  const d = rectOf(dialog)
  const shrunk = { x: d.x + d.w, y: r1.y, w: Math.max(0, r1.x + r1.w - (d.x + d.w)), h: r1.h }
  const ms = await T.invoke('panel_bounds', { rect: shrunk })
  await sleep(300)
  const shotOk = await T.invoke('shot', { name: 's6-3-shrunk-overlay' })
  await T.invoke('panel_bounds', { rect: r1 })
  return { setBoundsMs: ms, shrunk, shotOk, note: 'the guest reflows to the shrunk width while the dialog is up' }
})
dialog.style.display = 'none'; toast.style.display = 'none'
await finish()
