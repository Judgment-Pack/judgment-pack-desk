import type { ThemeChoice } from './deskConfig'

const ENTRY_THEME_KEY = 'jpack-desk:entry-theme'
const HANDOFF_KEY = 'jpack-desk:entry-theme-handoff:v1'

/** A fresh entry choice may cross into exactly one mounted workspace. */
export interface EntryThemeChoice {
  readonly theme: ThemeChoice
}

let pending: EntryThemeChoice | undefined
let handoffRead = false

/** The saved entry preference is for the public page, never a project default. */
export function readEntryTheme(): ThemeChoice {
  const handoff = pendingEntryTheme()
  if (handoff) return handoff.theme
  try {
    const stored = window.localStorage.getItem(ENTRY_THEME_KEY)
    if (stored === 'system' || stored === 'dark' || stored === 'light') return stored
  } catch { /* Private browsing may deny preference storage. */ }
  const current = document.documentElement.dataset.theme
  return current === 'dark' || current === 'light' ? current : 'system'
}

export function chooseEntryTheme(theme: ThemeChoice): void {
  handoffRead = true
  pending = { theme }
  // Sign-in reloads the document or leaves this origin for a provider. Keep
  // the fresh choice in this tab until a workspace accepts it, separately
  // from the public page's long-lived preference.
  try {
    window.sessionStorage.setItem(HANDOFF_KEY, theme)
  } catch { /* An in-place transition still works when storage is denied. */ }
  try {
    window.localStorage.setItem(ENTRY_THEME_KEY, theme)
  } catch { /* The handoff still preserves this visit's explicit choice. */ }
}

/** Reading during render must not consume it: StrictMode can initialize twice. */
export function pendingEntryTheme(): EntryThemeChoice | undefined {
  if (!handoffRead) {
    handoffRead = true
    try {
      const theme = window.sessionStorage.getItem(HANDOFF_KEY)
      if (theme === 'system' || theme === 'dark' || theme === 'light') pending = { theme }
    } catch { /* Keep any choice made in this document in memory. */ }
  }
  return pending
}

/** Acknowledge after commit without discarding a newer entry selection. */
export function acknowledgeEntryTheme(choice: EntryThemeChoice): void {
  if (pending !== choice) return
  pending = undefined
  try {
    if (window.sessionStorage.getItem(HANDOFF_KEY) === choice.theme) {
      window.sessionStorage.removeItem(HANDOFF_KEY)
    }
  } catch { /* The accepted choice stays in the workspace for this visit. */ }
}
