// Copies what the spike pages load from elsewhere into ui/ before cargo builds
// (include_dir! bakes ui/ into the binary). Never merge.
import { cpSync, existsSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const spike = join(here, '..')
const repo = join(spike, '..')
mkdirSync(join(spike, 'ui/vendor'), { recursive: true })
mkdirSync(join(spike, 'ui/src'), { recursive: true })
cpSync(join(spike, 'node_modules/vue/dist/vue.esm-browser.prod.js'), join(spike, 'ui/vendor/vue.esm-browser.prod.js'))
// S2 runs the frontend's own event bus, byte for byte.
cpSync(join(repo, 'frontend/src/useEventBus.js'), join(spike, 'ui/src/useEventBus.js'))
// S9: the real desktop renderer, when it has been built (desktop: vite build -c vite.renderer.config.mjs).
const renderer = join(repo, 'desktop/dist/renderer')
mkdirSync(join(spike, 'ui/renderer'), { recursive: true })
if (existsSync(renderer)) cpSync(renderer, join(spike, 'ui/renderer'), { recursive: true })
console.log('ui prepared', existsSync(renderer) ? '(with renderer)' : '(no renderer)')
