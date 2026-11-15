# The server's config, stored in the database

- **After the first start the database wins over yaml and env vars.** `app.Load`
  seeds each block of base.yaml into `_app_configurations` (encrypted with
  `auth.workspaceTokenKey`) only when its row is missing, then lays the rows
  over yaml. A new env var default does nothing on a server already running;
  a new *field* is added to the stored row with its yaml value at the next
  start, and a field removed from yaml is dropped from it, so the admin page
  lists exactly what the server reads.
- **A new block or setting needs no admin page work**: it is seeded and shows
  on `/admin/config`, whose forms are built from the stored JSON. A new
  startup check goes in `checkConfig` too, or an admin save can stop the next
  start.
- Env-only, never stored: `postgres`, `sqlite`, `idgen` (`envOnlyBlocks`) and
  `auth.rootAccessToken`, `auth.workspaceTokenKey` (`envOnlyFields`).
