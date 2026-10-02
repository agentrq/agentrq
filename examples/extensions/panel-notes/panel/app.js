// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// The page's half of Notes. It holds no data of its own: it asks its
// extension for the notes when it opens, and hands them back as they change.
// `window.agentrq.panel` exists only on an extension's own page.

const notes = document.getElementById('notes')
const status = document.getElementById('status')
const panel = window.agentrq?.panel

let timer = null

function say(text) {
  status.textContent = text
}

async function load() {
  if (!panel) return say('Open this page from the AgentRQ side panel.')
  try {
    const { text } = await panel.send({ type: 'load' })
    notes.value = text
    notes.disabled = false
    say('Saved on this computer')
  } catch (error) {
    say(error.message)
  }
}

// Saved a moment after typing stops, not on every key.
notes.addEventListener('input', () => {
  say('Saving…')
  clearTimeout(timer)
  timer = setTimeout(async () => {
    try {
      await panel.send({ type: 'save', text: notes.value })
      say('Saved on this computer')
    } catch (error) {
      say(error.message)
    }
  }, 400)
})

// A task added from its menu while this page is open.
panel?.onMessage((message) => {
  if (message?.type !== 'changed' || document.activeElement === notes) return
  notes.value = message.text
})

load()
