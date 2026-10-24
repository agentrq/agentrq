// WP0 spike helpers. Never merge.
export const T = window.__TAURI__.core
export const report = (key, value) => T.invoke('report', { key, value })
export const finish = () => T.invoke('finish')
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
export const median = (a) => { const s = [...a].sort((x, y) => x - y); return s.length ? s[Math.floor(s.length / 2)] : null }
window.addEventListener('error', (e) => report('jsError-' + Date.now(), String(e.message)))
window.addEventListener('unhandledrejection', (e) => report('jsRejection-' + Date.now(), String(e.reason?.stack || e.reason)))
export async function step(name, fn) {
  try { const v = await fn(); await report(name, v); return v } catch (e) { await report(name, { error: String(e?.stack || e) }); return null }
}
