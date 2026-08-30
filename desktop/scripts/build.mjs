import { build } from 'vite'
import { fileURLToPath, URL } from 'node:url'

const config = (name) => fileURLToPath(new URL(`../vite.${name}.config.mjs`, import.meta.url))

// Renderer first: it is the slowest and the most likely to fail, so a broken
// build surfaces before time is spent on the two small bundles.
for (const target of ['renderer', 'main', 'preload']) {
  console.log(`\n▸ building ${target}`)
  await build({ configFile: config(target) })
}

console.log('\n✓ desktop build complete')
