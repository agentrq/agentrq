/**
 * The extension manifest, and whether one is worth offering.
 *
 * An extension is a GitHub repository carrying the `agentrq-extension` topic and
 * an `agentrq-extension.json` at its root. The topic makes it findable; this
 * file is what makes it an extension. Everything downstream — the catalogue, the
 * installer, the grant screen — trusts what comes out of here, so this is
 * deliberately the most testable thing in the feature: pure functions, no
 * Electron, no network, no filesystem.
 *
 * ## Every rejection carries a reason, and the reason is copy
 *
 * A repository with the topic and a bad manifest is *listed as broken with its
 * reason* rather than quietly dropped — the author needs to see why, and a
 * silently-missing extension is undebuggable for them and looks like a broken
 * catalogue to anyone following a link. So the strings below are user-facing
 * text, addressed to the person who wrote the manifest, and are worth the same
 * care as anything else a person reads.
 *
 * ## Why validation is split in two
 *
 * `parseManifest` answers "is this a well-formed manifest", which depends on
 * nothing but the document. `checkCompatibility` answers "can *this* install
 * run it", which depends on the running app and on the tools the connected
 * server actually offers.
 *
 * They are separate because the answers have different lifetimes. A manifest is
 * parsed once at discovery, when no workspace is in view and no tool list is
 * known; compatibility is re-asked at install and again on update, against a
 * self-hosted backend that may be older than the extension expects. Folding them
 * together would mean either caching an answer that goes stale or refusing to
 * index anything until a server is reachable.
 */

/** Keys a manifest may carry. Anything else is a typo worth reporting. */
const KNOWN_KEYS = new Set([
  'name',
  'displayName',
  'version',
  'description',
  'license',
  'engines',
  'mcp',
  'net',
  'config',
  'provides',
  'shortcuts',
  'artifact',
])

/**
 * The name, which is an address rather than a label.
 *
 * It appears in a route (`/extensions/:name/:pageId`), in registry keys and in
 * the install directory, so the same discipline the workspace memory keys use
 * applies for the same reason: a name with a space in it is not one.
 */
const NAME_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/

/** Lowercase hex, 64 characters. Anything else could not be a SHA-256. */
const SHA256_RE = /^[a-f0-9]{64}$/

/**
 * SPDX identifiers we recognise, plus the honest refusal.
 *
 * An extension runs with full access to the machine, so the terms it is offered
 * under are part of the decision to install it rather than metadata — which is
 * why this is required rather than optional.
 *
 * It is a fixed list and case-sensitive on purpose. Accepting free text would
 * make `MIT`, `mit` and `MIT License` three different licenses and none of them
 * comparable; accepting any SPDX-shaped string would take `MITT` without
 * comment. The list is short enough to read and long enough to cover what people
 * actually publish under; an identifier missing from it is a one-line addition,
 * and the rejection says so rather than pretending the license is invalid.
 *
 * `UNLICENSED` is deliberately here. "All rights reserved, no permission
 * granted" is an answer a user can act on. Silence is not an answer; it just
 * leaves the question unasked.
 */
const SPDX = new Set([
  'AGPL-3.0-only',
  'AGPL-3.0-or-later',
  'Apache-2.0',
  'BSD-2-Clause',
  'BSD-3-Clause',
  'BSL-1.0',
  'CC0-1.0',
  'CC-BY-4.0',
  'CC-BY-SA-4.0',
  'EPL-2.0',
  'GPL-2.0-only',
  'GPL-2.0-or-later',
  'GPL-3.0-only',
  'GPL-3.0-or-later',
  'ISC',
  'LGPL-2.1-only',
  'LGPL-3.0-only',
  'LGPL-3.0-or-later',
  'MIT',
  'MPL-2.0',
  'Unlicense',
  'Zlib',
  'UNLICENSED',
])

const fail = (reason) => ({ ok: false, reason })

/** A trimmed string, or '' for anything that is not one. */
function str(value) {
  return typeof value === 'string' ? value.trim() : ''
}

/**
 * Validates the license, suggesting the right spelling where it can.
 *
 * The case-insensitive near-match is the whole reason this is worth a function:
 * `mit` is not a slip to reject flatly, it is somebody who meant `MIT`, and
 * saying so costs nothing and saves a round trip through a maintainer.
 */
export function validateLicense(value) {
  const license = str(value)
  if (!license) {
    return fail('"license" is required. Use an SPDX identifier such as "MIT", or "UNLICENSED".')
  }
  if (SPDX.has(license)) return { ok: true, license }

  const near = [...SPDX].find((id) => id.toLowerCase() === license.toLowerCase())
  if (near) {
    return fail(`"license" must be spelled exactly as the SPDX identifier: "${near}", not "${license}".`)
  }
  return fail(
    `"license" is not a recognised SPDX identifier: "${license}". ` +
      'Use one of the standard identifiers, or "UNLICENSED" if no permission is granted.',
  )
}

/**
 * Whether an app version satisfies a range.
 *
 * A deliberately narrow subset of semver — exact, `^`, `~` and `>=` — because
 * this project carries exactly one runtime dependency and re-implementing the
 * whole grammar to avoid a second one would trade a dependency for a class of
 * subtle bugs. (`semver` is present transitively today, which is not the same as
 * being available.)
 *
 * Anything outside the subset is **refused as unsupported rather than guessed
 * at**. A range nobody parsed correctly is how an extension ends up installed on
 * a version it cannot run on, and the failure then surfaces somewhere far away
 * from the manifest that caused it.
 */
export function satisfiesRange(version, range) {
  const v = parseVersion(version)
  const spec = str(range)
  if (!v) return fail(`"${version}" is not a version this can compare.`)
  if (!spec) return fail('"engines.agentrq" is required, for example "^1.4".')

  const operator = spec.startsWith('>=') ? '>=' : spec[0] === '^' || spec[0] === '~' ? spec[0] : ''
  const bound = parseVersion(spec.slice(operator.length))
  if (!bound) {
    return fail(
      `"engines.agentrq" is not a range this understands: "${spec}". ` +
        'Supported forms are "1.4.0", "^1.4", "~1.4" and ">=1.4".',
    )
  }

  if (compare(v, bound) < 0) return { ok: true, satisfied: false }

  // ^ allows anything up to the next major; ~ up to the next minor; >= has no
  // ceiling; an exact version means exactly itself.
  if (operator === '>=') return { ok: true, satisfied: true }
  if (operator === '^') return { ok: true, satisfied: v.major === bound.major }
  if (operator === '~') return { ok: true, satisfied: v.major === bound.major && v.minor === bound.minor }
  return { ok: true, satisfied: compare(v, bound) === 0 }
}

/** `1.4` and `1.4.2` both parse; a missing part is zero. */
function parseVersion(value) {
  const match = /^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?$/.exec(str(value))
  if (!match) return null
  return { major: +match[1], minor: +(match[2] ?? 0), patch: +(match[3] ?? 0) }
}

function compare(a, b) {
  return a.major - b.major || a.minor - b.minor || a.patch - b.patch
}

/** The tools an extension asks for, on one of the two MCP surfaces. */
function validateToolList(value, surface) {
  if (value === undefined) return { ok: true, tools: [] }
  if (!Array.isArray(value)) {
    return fail(`"mcp.${surface}" must be a list of tool names.`)
  }
  const tools = []
  for (const entry of value) {
    const tool = str(entry)
    if (!tool) return fail(`"mcp.${surface}" contains an entry that is not a tool name.`)
    if (tools.includes(tool)) return fail(`"mcp.${surface}" lists "${tool}" twice.`)
    tools.push(tool)
  }
  return { ok: true, tools }
}

/** The release asset an install downloads, and the digest that pins it. */
function validateArtifact(value) {
  if (!value || typeof value !== 'object') {
    return fail('"artifact" is required, naming the release, the asset and its sha256.')
  }
  const release = str(value.release)
  const asset = str(value.asset)
  const sha256 = str(value.sha256).toLowerCase()

  if (!release) return fail('"artifact.release" is required, for example "v1.2.0".')
  if (!asset) return fail('"artifact.asset" is required — the file name of the release asset.')
  if (!SHA256_RE.test(sha256)) {
    // Worth its own message: this is the field that makes an install honest, and
    // a git tag can be moved under one that is already in place.
    return fail('"artifact.sha256" must be a 64-character hex SHA-256 of the asset.')
  }
  return { ok: true, artifact: { release, asset, sha256 } }
}

/**
 * Hosts the author says the extension will contact.
 *
 * **This is a declaration, not a restriction, and nothing anywhere enforces it.**
 * An extension is trusted Node code — it can open any socket it likes, and this
 * field changes nothing about that. Saying otherwise on an install screen would
 * be worse than saying nothing, because a list of hosts under a heading that
 * reads like permissions is read as a boundary.
 *
 * It is here because it is still worth knowing. An extension that says it talks
 * to `hooks.slack.com` has told the user something true and useful about what it
 * is for, and one that says nothing and clearly does reach the network is a
 * mismatch a reviewer can see. The install screen presents it as part of the
 * author's description of their own extension, alongside the sentence about full
 * machine access — never as a list of things it is limited to.
 *
 * Hostnames only: no scheme, no path, no port. A wildcard is allowed at the
 * front (`*.example.com`) because an API spread over subdomains is ordinary, and
 * listing forty of them would tell a reader less than one line does.
 */
const HOST_RE = /^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/

function validateNet(value) {
  if (value === undefined) return { ok: true, net: [] }
  if (!Array.isArray(value)) return fail('"net" must be a list of hostnames.')

  const net = []
  for (const entry of value) {
    const host = str(entry).toLowerCase()
    if (!HOST_RE.test(host)) {
      // Named, because the mistake is nearly always a URL pasted whole and the
      // fix is to delete everything but the host.
      return fail(`"net" contains "${str(entry)}", which is not a hostname. Use "api.example.com", with no scheme or path.`)
    }
    if (net.includes(host)) return fail(`"net" lists "${host}" twice.`)
    net.push(host)
  }
  return { ok: true, net }
}

/** Config fields an extension asks the user to fill in. */
function validateConfig(value) {
  if (value === undefined) return { ok: true, config: [] }
  if (!Array.isArray(value)) return fail('"config" must be a list of fields.')

  const config = []
  const seen = new Set()
  for (const field of value) {
    if (!field || typeof field !== 'object') return fail('"config" contains an entry that is not a field.')
    const key = str(field.key)
    const type = str(field.type) || 'string'
    if (!key) return fail('Every "config" field needs a "key".')
    if (seen.has(key)) return fail(`"config" declares "${key}" twice.`)
    if (!['string', 'secret', 'boolean', 'number'].includes(type)) {
      return fail(`"config.${key}" has an unknown type "${type}". Use string, secret, boolean or number.`)
    }
    seen.add(key)
    config.push({ key, type, label: str(field.label) || key })
  }
  return { ok: true, config }
}

/**
 * Reads a manifest and says whether it is one.
 *
 * Structure only — nothing here depends on the running app or on any server. See
 * `checkCompatibility` for the half that does.
 */
export function parseManifest(source) {
  let raw = source
  if (typeof source === 'string') {
    try {
      raw = JSON.parse(source)
    } catch {
      return fail('This is not valid JSON.')
    }
  }
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return fail('A manifest must be a JSON object.')
  }

  // Reported rather than ignored: a mistyped key is an author expecting
  // behaviour they will not get, and silence is the one response that guarantees
  // they never find out.
  const unknown = Object.keys(raw).filter((key) => !KNOWN_KEYS.has(key))
  if (unknown.length > 0) {
    return fail(`Unknown field${unknown.length > 1 ? 's' : ''}: ${unknown.map((k) => `"${k}"`).join(', ')}.`)
  }

  const name = str(raw.name)
  if (!name) return fail('"name" is required.')
  if (!NAME_RE.test(name)) {
    return fail(
      `"name" must be lowercase words joined by single hyphens, like "linear-issues". Got "${name}".`,
    )
  }

  const version = str(raw.version)
  if (!parseVersion(version)) {
    return fail(`"version" must be a version like "1.2.0". Got "${version || '(missing)'}".`)
  }

  const license = validateLicense(raw.license)
  if (!license.ok) return license

  const engines = raw.engines && typeof raw.engines === 'object' ? raw.engines : {}
  const agentrq = str(engines.agentrq)
  if (!agentrq) {
    return fail('"engines.agentrq" is required, so an incompatible build can say so before installing.')
  }
  // Parsed now, against a version known to be valid, purely to reject an
  // unsupported range at discovery rather than at install.
  const rangeCheck = satisfiesRange('0.0.0', agentrq)
  if (!rangeCheck.ok) return rangeCheck

  const mcp = raw.mcp && typeof raw.mcp === 'object' ? raw.mcp : {}
  const workspace = validateToolList(mcp.workspace, 'workspace')
  if (!workspace.ok) return workspace
  const supervisor = validateToolList(mcp.supervisor, 'supervisor')
  if (!supervisor.ok) return supervisor

  const net = validateNet(raw.net)
  if (!net.ok) return net

  const config = validateConfig(raw.config)
  if (!config.ok) return config

  const artifact = validateArtifact(raw.artifact)
  if (!artifact.ok) return artifact

  return {
    ok: true,
    manifest: {
      name,
      displayName: str(raw.displayName) || name,
      version,
      description: str(raw.description),
      license: license.license,
      engines: { agentrq },
      mcp: { workspace: workspace.tools, supervisor: supervisor.tools },
      net: net.net,
      config: config.config,
      provides: raw.provides && typeof raw.provides === 'object' ? raw.provides : {},
      shortcuts: Array.isArray(raw.shortcuts) ? raw.shortcuts : [],
      artifact: artifact.artifact,
    },
  }
}

/**
 * Whether this install can actually run a parsed manifest.
 *
 * Answered against the app that is running and the tools the connected servers
 * actually advertise — which is the real check, because a self-hosted backend
 * may be older than the extension expects and a tool an extension asks for may
 * simply not exist there.
 *
 * Returns every reason rather than the first. An author fixing a manifest wants
 * the whole list, and a user being told why something is unavailable is better
 * served by "needs AgentRQ 1.5, you have 1.4" plus the missing tool than by
 * whichever happened to be checked first.
 */
export function checkCompatibility(manifest, { appVersion, workspaceTools = [], supervisorTools = [] } = {}) {
  const reasons = []

  const range = satisfiesRange(appVersion, manifest.engines.agentrq)
  if (!range.ok) {
    reasons.push(range.reason)
  } else if (!range.satisfied) {
    reasons.push(`Needs AgentRQ ${manifest.engines.agentrq}; this is ${appVersion}.`)
  }

  const missing = (asked, offered, surface) =>
    asked
      .filter((tool) => !offered.includes(tool))
      .forEach((tool) => reasons.push(`The ${surface} server does not offer "${tool}".`))

  missing(manifest.mcp.workspace, workspaceTools, 'workspace')
  missing(manifest.mcp.supervisor, supervisorTools, 'supervisor')

  return { compatible: reasons.length === 0, reasons }
}

/** Exported for the tests, and as the readable statement of what is accepted. */
export const SPDX_IDENTIFIERS = SPDX
