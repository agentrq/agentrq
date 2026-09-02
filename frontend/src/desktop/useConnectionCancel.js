import { ref } from 'vue'

/**
 * Backing out of the connection screen.
 *
 * Adding a profile opens a window on a session that has never signed in, so it
 * lands on the connection screen — and until now the only ways off it were
 * finishing the setup or quitting the app. This is the way back.
 *
 * The shell does the work: it discards the half-made profile and replaces the
 * window with the previous profile's. So the successful case is one where
 * nothing here runs afterwards, and the only states worth modelling are the
 * two failures — the shell declining, and the call itself failing.
 *
 * Kept out of the component because those two are exactly what a template
 * cannot express and a test cannot reach.
 */

/** Shown when the shell says there is nowhere to go back to. */
export const NO_PROFILE_TO_RETURN_TO = 'There is no other profile to go back to.'

/**
 * @returns {{cancelling: import('vue').Ref<boolean>, run: () => Promise<string>}}
 *          `run` resolves to '' when the window is on its way out, and
 *          otherwise to a message to show the user
 */
export function useConnectionCancel() {
  const cancelling = ref(false)

  async function run() {
    // A second press while the first is in flight would ask the shell to
    // discard a profile it is already discarding.
    if (cancelling.value) return ''

    cancelling.value = true
    try {
      // Deliberately stays true on success: the shell is replacing this
      // window, and re-enabling the buttons for those few frames would only
      // offer an action that no longer means anything.
      if (await window.agentrq.connection.cancel()) return ''

      cancelling.value = false
      return NO_PROFILE_TO_RETURN_TO
    } catch (err) {
      cancelling.value = false
      return String(err?.message ?? err)
    }
  }

  return { cancelling, run }
}
