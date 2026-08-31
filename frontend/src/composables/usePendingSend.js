import { ref } from 'vue';

/**
 * The hold-before-send countdown behind the composer's send delay.
 *
 * The rule this exists to enforce: **a held message belongs to the task it was
 * written in.** The task detail view is reused across tasks — switching tasks
 * changes a route parameter rather than remounting — so a countdown started in
 * one task was still running when the next one appeared, and delivered against
 * whichever task was on screen when it reached zero. A message could be written
 * for one task and posted to another.
 *
 * So the target is captured when the send is started and travels with it. The
 * countdown never asks what is on screen now.
 *
 * Navigating away resolves the hold rather than leaving it running: `flush`
 * delivers it to the task it was addressed to. The delay is a chance to catch a
 * mistake while you are looking at it, and once you have moved on there is
 * nothing left to catch — but the message was still asked for, and dropping it
 * silently is how people lose work they thought they had sent.
 *
 * Timers are injected so the whole state machine can be tested without waiting.
 *
 * @param {object} deps
 * @param {(pending: object) => void} deps.deliver  sends it, to `pending.target`
 * @param {typeof setInterval} [deps.setTimer]
 * @param {typeof clearInterval} [deps.clearTimer]
 */
export function usePendingSend({ deliver, setTimer = setInterval, clearTimer = clearInterval }) {
  /** @type {import('vue').Ref<null | {text: string, atts: Array, secondsLeft: number, target: object, timerId: any}>} */
  const pending = ref(null);

  function discard() {
    if (pending.value?.timerId != null) clearTimer(pending.value.timerId);
    pending.value = null;
  }

  /**
   * Begin holding a message for `seconds`, addressed to `target`.
   * Any message already being held is delivered first, so one can never
   * silently replace another.
   */
  function start({ text, atts, seconds, target }) {
    flush();

    pending.value = { text, atts, secondsLeft: seconds, target, timerId: null };
    pending.value.timerId = setTimer(() => {
      if (!pending.value) return;
      pending.value.secondsLeft -= 1;
      if (pending.value.secondsLeft <= 0) flush();
    }, 1000);
  }

  /**
   * Deliver it now, to the task it was written in.
   *
   * @returns {object | null} what was delivered, or null if nothing was held
   */
  function flush() {
    const held = pending.value;
    if (!held) return null;
    discard();
    deliver(held);
    return held;
  }

  /**
   * Take it back, unsent.
   *
   * @returns {object | null} what was held, so the caller can put it back in
   *                          the composer, or null if nothing was held
   */
  function cancel() {
    const held = pending.value;
    if (!held) return null;
    discard();
    return held;
  }

  /** Stop the countdown without delivering. Teardown only. */
  function stop() {
    discard();
  }

  return { pending, start, flush, cancel, stop };
}
