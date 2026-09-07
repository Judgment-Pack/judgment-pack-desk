/**
 * The shape every project-file card's Save has, held in one place.
 *
 * Four cards write four members of one file, and the four forms differ only in
 * their fields. Everything a reader can get wrong about a save is the same on
 * all of them, so it is here rather than four times: what Save is disabled by,
 * what a refusal looks like, what a file that moved on disk offers, and which
 * of a save's states may stand beside which.
 *
 * **Save is disabled by a comparison, never by a flag.** A remembered "has been
 * edited" stays true after an edit somebody undid, so a form would offer to
 * write a file it would not change. The same comparison decides whether a fresh
 * read may take over the fields — which is what makes Reload after a refused
 * write keep every value that was typed, and what makes a save that lands
 * re-seed them from the file the chassis read back.
 *
 * **The fields are disabled while a write is in flight**, because a field
 * edited between the request and its answer is a value the author believes was
 * saved and was not — and while this page has no bytes to write over, when they
 * hold the built-in defaults rather than anything anybody configured.
 *
 * **A refusal is rendered in the decoder's own words**, against the field its
 * key path names, and whole where it names no field on this form: a problem
 * with the file this save would have made is still a problem, and dropping it
 * would leave a refusal with no sentence.
 */
import { useState, type ReactNode } from 'react'
import { AlertPanel } from '../ui/AlertPanel'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import {
  useProjectFileSave,
  type MemberEdit,
  type ProjectFilePointer,
  type ProjectFileSave
} from './useProjectFileSave'

/** One card's draft, and everything the shell below needs to render it. */
export interface ProjectFileDraft<D> {
  draft: D
  /** Take an edited draft. Clears the last save's verdict and nothing else. */
  set: (next: D) => void
  /** Whether any touched field says something the file does not. */
  changed: boolean
  /** Write the fields the reader touched and changed, and nothing else. */
  submit: () => void
  save: ProjectFileSave
}

/**
 * The fields the reader has typed into, by name — and only those.
 *
 * **A whole draft is not a record of what anybody edited**, and the difference
 * is a lost edit. Round 1 of the review found it: with one snapshot of every
 * field, edit Name while another writer adds a `mark` on disk, take the 409,
 * press Reload — and the snapshot still carries the `mark` this reader last
 * saw, which was none. The next Save then states the fresh digest and writes
 * `mark: null`, erasing a change nobody here ever looked at, under a
 * precondition that is now perfectly true.
 *
 * A value equal to the seed's is not held: an edit somebody undid is not an
 * edit, and neither is a field a control re-emitted unchanged.
 */
function touchedIn<D>(next: D, seed: D): Record<string, unknown> {
  const held: Record<string, unknown> = {}
  for (const [name, value] of Object.entries(next as Record<string, unknown>)) {
    if (!Object.is(value, (seed as Record<string, unknown>)[name])) held[name] = value
  }
  return held
}

/**
 * One card's draft: the file's own values, under the fields the reader has
 * typed into.
 *
 * **The draft is derived, not stored**, and that is the whole of the rule. A
 * field nobody has touched *is* whatever the file says now — a save that
 * landed, a reload, another editor's change all reach it with nothing to
 * decide — and a field somebody has typed into is theirs until it is written or
 * withdrawn. There is no re-seeding step, because there is nothing stale to
 * re-seed: the only thing this hook remembers is what a person actually typed.
 *
 * That also makes the edit list exact. Every untouched name in `draft` is the
 * seed's own value, so a card's `editsOf` — which compares the two field by
 * field — can only report what the reader changed, whatever has happened to the
 * file in between.
 *
 * The values are the values of form controls: strings, and the members of the
 * decoder's closed unions. `Object.is` is the comparison because that is what
 * they admit; nothing here holds an object.
 */
export function useProjectFileDraft<D>(
  pointer: ProjectFilePointer,
  seed: D,
  editsOf: (draft: D, seed: D) => MemberEdit[]
): ProjectFileDraft<D> {
  const [touched, setTouched] = useState<Record<string, unknown>>({})
  const draft = { ...seed, ...touched } as D
  // Computed from the draft above, so an untouched field contributes nothing
  // however far the file has moved since it was last looked at.
  const edits = editsOf(draft, seed)
  const changed = edits.length > 0
  // **What the fields hold is what decides whether the revision may move.** A
  // card holding a value nobody has written keeps the bytes and the digest it
  // was composed against, so a change made underneath it is refused rather than
  // overwritten. See `useProjectFileSave`.
  const save = useProjectFileSave(pointer, changed)
  return {
    draft,
    set: (next: D) => {
      save.forget()
      setTouched(touchedIn(next, seed))
    },
    changed,
    submit: () => save.save(edits),
    save
  }
}

/**
 * The form around one card's fields: the fieldset, the Save, and every state a
 * save can end in.
 */
export function ProjectFileForm<D>({
  state,
  placed,
  children
}: {
  state: ProjectFileDraft<D>
  /** The key paths this card renders beside a field. Everything else is whole. */
  placed: readonly string[]
  children: ReactNode
}) {
  const { save, changed } = state
  const unplaced = save.problems.filter((problem) => !placed.includes(problem.key))
  return (
    <form
      // **The decoder is the one thing that refuses a value here.** A number
      // field carries the decoder's own bounds as `min` and `max`, which is
      // what makes the spinner stop where the file does — and those attributes
      // also make the browser refuse the submit before this form ever sees it,
      // with a bubble in wording this desk did not write and cannot show the
      // decoder's sentence beside. The live drive is what found it: a 20000px
      // Inspector produced no refusal at all, because nothing had been asked.
      //
      // Nothing in the suite can hold this: jsdom performs no constraint
      // validation, so a form without `noValidate` behaves there exactly as one
      // with it. It is stated here and proved in a browser.
      noValidate
      onSubmit={(event) => {
        event.preventDefault()
        state.submit()
      }}
    >
      <fieldset disabled={save.pending || !save.ready}>
        {children}
        <p className="actions">
          <Button
            variant="primary"
            type="submit"
            disabled={!changed || save.pending || save.reloading}
          >
            Save
          </Button>{' '}
          {save.pending && <span className="quiet">writing…</span>}
          {save.reloading && <span className="quiet">reading…</span>}
          {save.said !== undefined && !save.pending && (
            <span className="quiet">{save.said}</span>
          )}
        </p>
      </fieldset>

      {save.blocked !== undefined && <p className="quiet">{save.blocked}</p>}

      {save.stale !== undefined && (
        <AlertPanel
          heading="The file changed on disk — nothing was written."
          detailLabel="digests"
          detail={
            <>
              <span>
                this page read{' '}
                <code title={save.stale.expectedSha256}>
                  sha256 {short(save.stale.expectedSha256)}
                </code>
              </span>
              <span>
                on disk now{' '}
                <code title={save.stale.actualSha256}>sha256 {short(save.stale.actualSha256)}</code>
              </span>
            </>
          }
          actions={
            <Button
              variant="primary"
              disabled={save.pending || save.reloading}
              onClick={() => save.reload()}
            >
              Reload
            </Button>
          }
        >
          <span>
            Everything typed here is still here. Reload reads the file again, so the next Save
            states a digest that is true.
          </span>
        </AlertPanel>
      )}

      {unplaced.length > 0 && (
        <div role="alert">
          <p>This value was refused, and nothing was written.</p>
          {unplaced.map((problem) => (
            <code key={`${problem.key}:${problem.reason}`} className="partial-reason">
              {problem.key === '' ? problem.reason : `${problem.key}: ${problem.reason}`}
            </code>
          ))}
        </div>
      )}

      {save.refusal !== undefined && <Alert reason={save.refusal}>Nothing was written.</Alert>}
    </form>
  )
}

/** The problem the decoder named at one key path, where it named one. */
export function problemAt(
  save: ProjectFileSave,
  key: string
): string | undefined {
  return (
    save.problems
      .filter((problem) => problem.key === key)
      .map((problem) => problem.reason)
      .join(' ') || undefined
  )
}

function short(value: string): string {
  return value ? `${value.slice(0, 12)}…` : '(no file)'
}
