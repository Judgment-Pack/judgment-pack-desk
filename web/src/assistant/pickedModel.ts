/** Per-tab model preferences, scoped to a project, AI connection, and feature.
 * Picks outside the enabled set fall back to that connection's default.
 * Storage failures never prevent using the configured default.
 */
import { useState } from 'react'
import { projectKey } from '../shell/paneState'

/**
 * The record version, in the key.
 *
 * A key rather than a member, on `paneState`'s precedent: a record this build
 * cannot read is never read again rather than half-honoured.
 */
const RECORD_VERSION = 1

/** Where one project's pick is remembered, on this tab. */
export function pickedModelKey(projectRoot: string | undefined, connectionId?: string, surface = 'assistant'): string {
  return connectionId
    ? `jpack-desk:model:v2:${JSON.stringify([projectKey(projectRoot), connectionId, surface])}`
    : `jpack-desk:model:v${RECORD_VERSION}:${projectKey(projectRoot)}`
}

/**
 * The pick this tab remembers, where it is still one of the enabled models.
 *
 * `undefined` for every other outcome: nothing stored, storage that refused to
 * answer, a value that is not a string, and a value the set no longer holds.
 * They are one answer because they have one repair — take the default.
 */
export function readPickedModel(key: string, models: readonly string[]): string | undefined {
  let held: string | null = null
  try {
    held = window.sessionStorage.getItem(key)
  } catch {
    return undefined
  }
  if (held === null || !models.includes(held)) return undefined
  return held
}

/** Remember one pick, or leave the tab working where storage refuses. */
export function writePickedModel(key: string, model: string): void {
  try {
    window.sessionStorage.setItem(key, model)
  } catch {
    // A viewer whose storage refuses writes still gets a working tab; the pick
    // simply does not survive a navigation, which is what they asked for.
  }
}

export interface PickedModel {
  /** The id this run would use, or `''` where nothing is enabled. */
  model: string
  /** The models this endpoint enables, in the file's own order. */
  models: readonly string[]
  pick: (model: string) => void
}

/** Resolve the selected model without carrying it into a different connection. */
export function usePickedModel(
  models: readonly string[],
  fallback: string | null,
  projectRoot: string | undefined,
  connectionId?: string,
  surface = 'assistant'
): PickedModel {
  const key = pickedModelKey(projectRoot, connectionId, surface)
  const [held, setHeld] = useState(() => ({ key, picked: readPickedModel(key, models) }))
  // Reset synchronously on a project/connection change. Identical model IDs do
  // not make two providers interchangeable, even for the first render.
  const current = held.key === key ? held : { key, picked: readPickedModel(key, models) }
  if (held.key !== key) setHeld(current)
  const model = current.picked !== undefined && models.includes(current.picked) ? current.picked : (fallback ?? '')
  return {
    model, models,
    pick: (next: string) => {
      if (!models.includes(next)) return
      setHeld({ key, picked: next })
      writePickedModel(key, next)
    }
  }
}
