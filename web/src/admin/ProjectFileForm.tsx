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
  /** Whether the draft differs from the value the file supplies. */
  changed: boolean
  /** Write the fields that differ, and nothing else. */
  submit: () => void
  save: ProjectFileSave
}

/**
 * One card's draft, seeded from the file and re-seeded only where nothing would
 * be lost by it.
 *
 * The read has usually not answered at first render, and a save answers with
 * the file the chassis read back — both have to reach the fields. An edit in
 * progress must not, which is the same rule that makes Reload after a refused
 * write keep what somebody typed. Adjusted during render rather than in an
 * effect, so the fields are never painted a frame behind the file.
 *
 * **Two comparisons and no remembered flag**, and each admits a fresh seed for
 * its own reason. `drafted === seeded` is "nothing has been typed since the
 * last seed", which is the first render, an answer that arrives before anybody
 * touches the form, and an edit somebody undid. `!changed` is "the file now
 * says exactly what these fields do", which is what a save that landed
 * produces — and without it the form would take that answer and then never take
 * another, because it would go on comparing against a seed two revisions old.
 * A draft that says something the file does not is what neither admits, and
 * that is the case Reload exists for.
 */
export function useProjectFileDraft<D>(
  pointer: ProjectFilePointer,
  seed: D,
  editsOf: (draft: D, seed: D) => MemberEdit[]
): ProjectFileDraft<D> {
  const identity = JSON.stringify(seed)
  const [seeded, setSeeded] = useState(identity)
  const [draft, setDraft] = useState<D>(seed)
  const drafted = JSON.stringify(draft)
  const changed = drafted !== identity
  // **What the fields hold is what decides whether the revision may move.** A
  // card holding a value nobody has written keeps the bytes and the digest it
  // was composed against, so a change made underneath it is refused rather than
  // overwritten. See `useProjectFileSave`.
  const save = useProjectFileSave(pointer, changed)
  if (identity !== seeded && (drafted === seeded || !changed)) {
    setSeeded(identity)
    setDraft(seed)
  }
  return {
    draft,
    set: (next: D) => {
      save.forget()
      setDraft(next)
    },
    changed,
    submit: () => save.save(editsOf(draft, seed)),
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
