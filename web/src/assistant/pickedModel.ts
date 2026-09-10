/**
 * Which of the enabled models this run uses — **a preference, per tab, and
 * never a file**.
 *
 * **The decision moved to where the decision is.** Admin says which models this
 * desk may run; a person at the Assistant tab says which one *this* piece of
 * work wants, and that is not a fact about the desk's configuration. Writing it
 * to `jpack-desk.json` would put a per-run choice into a file that names where a
 * credential is presented, and make one tab's pick the other tab's default.
 *
 * **`sessionStorage`, on the shell's own precedent**: per tab, cleared when the
 * tab closes, keyed on the project root the chassis pinned so two projects open
 * in two tabs do not share one. `localStorage` would outlive the session and
 * cross the tabs, which is exactly what a per-run preference must not do.
 *
 * **Every access is in a try/catch**, and not defensively: a private window or
 * a browser set to block site data *throws on the accessor itself* rather than
 * answering null, and a thrown accessor must still leave a working tab.
 *
 * **And a remembered pick outside the enabled set is treated as absent.** The
 * set moves — a model is unticked in Admin, an endpoint is replaced — and a run
 * on an id this desk is no longer configured for would be a request nobody
 * enabled, made on the strength of something a browser remembered. What stands
 * in its place is the default, which is the set's own answer to "where does a
 * run start".
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
export function pickedModelKey(projectRoot: string | undefined): string {
  return `jpack-desk:model:v${RECORD_VERSION}:${projectKey(projectRoot)}`
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

/**
 * The picked model for one endpoint, on one project, in this tab.
 *
 * Seeded once from storage — a lazy initializer, so a read that throws does so
 * inside the initializer and not during a render — and re-derived where the set
 * moves under it: the state is kept, and a pick the set no longer holds simply
 * stops being what is returned.
 */
export function usePickedModel(
  models: readonly string[],
  fallback: string | null,
  projectRoot: string | undefined
): PickedModel {
  const key = pickedModelKey(projectRoot)
  const [picked, setPicked] = useState<string | undefined>(() => readPickedModel(key, models))
  // **Derived at every render rather than corrected in an effect.** The set can
  // change between renders — a save in Admin, a new endpoint — and an effect
  // would leave one frame in which the run is about to use a model nothing
  // enables.
  const model = picked !== undefined && models.includes(picked) ? picked : (fallback ?? '')
  return {
    model,
    models,
    pick: (next: string) => {
      // Only a member of the set, which is the same rule the read applies: a
      // control offers these and nothing else, and this is the layer under it.
      if (!models.includes(next)) return
      setPicked(next)
      writePickedModel(key, next)
    }
  }
}
