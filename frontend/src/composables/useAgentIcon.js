// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * An agent's own icon, from the name it gave itself.
 *
 * The name is whatever the agent said it was ("claude-code",
 * "codex-mcp-client", "gemini-cli-mcp-client" cut to 16 characters), so it is
 * matched by the words in it rather than in full. A word is matched whole: a
 * short name like "pi" or "amp" would otherwise turn up inside other names.
 *
 * SVGs come as markup, to draw inline: most are one colour, `currentColor`,
 * and as an `<img>` that is black on the dark theme. PNGs come as a URL.
 */

const svgs = import.meta.glob('../assets/agent-icons/*.svg', { eager: true, query: '?raw', import: 'default' });
const pngs = import.meta.glob('../assets/agent-icons/*.png', { eager: true, query: '?no-inline', import: 'default' });

// The words that name each icon's agent, most specific first: the first icon
// with a match wins.
const ICONS = [
  ['claude-agent.png', ['claude']],
  ['codex-cli.png', ['codex']],
  ['gemini-cli.png', ['gemini']],
  ['github-copilot.svg', ['copilot']],
  ['opencode.png', ['opencode']],
  ['openhands.png', ['openhands']],
  ['openclaw.png', ['openclaw']],
  ['cursor.png', ['cursor']],
  ['qwen-code.svg', ['qwen']],
  ['kimi-cli.svg', ['kimi']],
  ['kiro-cli.png', ['kiro']],
  ['kilo.svg', ['kilo']],
  ['goose.png', ['goose']],
  ['cline.png', ['cline']],
  ['junie.png', ['junie']],
  ['amp.svg', ['amp']],
  ['augment-code.png', ['augment', 'auggie']],
  ['antigravity.png', ['antigravity']],
  ['autohand-code.svg', ['autohand']],
  ['blackbox-ai.png', ['blackbox']],
  ['bub.png', ['bub']],
  ['codebuddy-code.svg', ['codebuddy']],
  ['command-code.png', ['command code']],
  ['cortex-code.svg', ['cortex']],
  ['corust-agent.svg', ['corust']],
  ['crow-cli.svg', ['crow']],
  ['deepagents.svg', ['deepagents']],
  ['devin.svg', ['devin']],
  ['dimcode.svg', ['dimcode']],
  ['dirac.svg', ['dirac']],
  ['docker-cagent.png', ['cagent']],
  ['factory-droid.png', ['droid']],
  ['fast-agent.png', ['fast agent']],
  ['fount.svg', ['fount']],
  ['glm-agent.svg', ['glm']],
  ['grok-build.svg', ['grok']],
  ['harn.svg', ['harn']],
  ['hermes-agent.png', ['hermes']],
  ['minion-code.svg', ['minion', 'mcode']],
  ['mistral-vibe.png', ['vibe', 'mistral']],
  ['nova.svg', ['nova']],
  ['pi.svg', ['pi']],
  ['poolside.svg', ['poolside']],
  ['qoder-cli.png', ['qoder']],
  ['sigit-code.svg', ['sigit']],
  ['stakpak.svg', ['stakpak']],
  ['stdio-bus.png', ['stdio bus']],
  ['vt-code.svg', ['vt code', 'vtcode']],
].map(([file, names]) => [file, names.map((n) => n.split(' '))]);

function words(name) {
  return String(name ?? '').toLowerCase().split(/[^a-z0-9]+/).filter(Boolean);
}

function contains(haystack, needle) {
  for (let i = 0; i + needle.length <= haystack.length; i++) {
    if (needle.every((w, j) => haystack[i + j] === w)) return true;
  }
  return false;
}

/**
 * The icon for an agent's name: `{ svg }` markup or `{ src }` URL, or null for
 * an agent there is no icon for.
 */
export function agentIconFor(name) {
  const ws = words(name);
  const hit = ICONS.find(([, names]) => names.some((n) => contains(ws, n)));
  if (!hit) return null;
  const key = `../assets/agent-icons/${hit[0]}`;
  return hit[0].endsWith('.svg') ? { svg: svgs[key] } : { src: pngs[key] };
}

/** Every icon file the names point at, for a test to hold against the folder. */
export const agentIconFiles = ICONS.map(([file]) => file);
