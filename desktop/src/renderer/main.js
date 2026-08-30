/**
 * Desktop renderer entry.
 *
 * It bootstraps the *actual* web frontend — same App.vue, same router, same
 * stores — because `@app` is aliased to ../frontend/src. There is deliberately
 * no second route table here: a view added to the frontend appears in the
 * desktop app with no change on this side.
 *
 * Phase 2 (task 0hua7LpaQID) replaces this side-effect import with an explicit
 * `createAgentRQApp({ history, platform })` factory, which is what gives
 * components a way to branch on the desktop platform. Until then the frontend's
 * own bootstrap is reused verbatim, which is the strongest parity guarantee
 * available without touching frontend code.
 */
import '@app/main.js'
