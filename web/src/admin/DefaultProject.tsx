/**
 * The Project card's one field and its Save: which project this desk opens
 * when it is launched without a directory.
 *
 * **The root is pinned per process, and this card never claims otherwise.**
 * Saving this changes the *next* launch and nothing about the desk in front of
 * you — the chassis resolved a project root once, at startup, and every part of
 * it operates on that one. So the line under the field says both: what the
 * value is for, and which directory this launch is actually on.
 *
 * The write is the same conditional commit the assistant slot is written under:
 * the digest this page read, a 409 that keeps what was typed and offers Reload,
 * and a 422 rendered in the decoder's own words against the key it names.
 *
 * **A hook returning two pieces of the card**, because they sit in two of its
 * slots and share one draft: a field that did not know a write was in flight
 * would let somebody edit between the request and its answer, which is a value
 * they believe was saved and was not.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { FileRequestError, StaleWrite } from '../files/client'
import { Alert } from '../ui/Alert'
import { AlertPanel } from '../ui/AlertPanel'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { CardField } from './SourceCard'
import { useUpdateProjectDefault } from './projectDefault'

const NO_DIGEST =
  'This desk has not read its own configuration file, and a write states the bytes it replaces.'

const SAVED = 'Saved. It applies to the next launch without a directory.'
const CLEARED = 'Saved. This desk configures no default project.'

export function useDefaultProject(): { field: ReactNode; save: ReactNode } {
  const { config, desk } = useEffectiveConfig()
  const client = useQueryClient()
  const write = useUpdateProjectDefault()
  const saved = config.project.file ?? ''
  const [seeded, setSeeded] = useState(saved)
  const [draft, setDraft] = useState(saved)
  const [dirty, setDirty] = useState(false)
  const [said, setSaid] = useState<string | undefined>(undefined)
  // Adjusted during render rather than in an effect, so the field is never
  // painted a frame behind the file — and never over an edit in progress.
  if (!dirty && saved !== seeded) {
    setSeeded(saved)
    setDraft(saved)
  }

  const digest = desk?.sha256
  const refused = write.error instanceof FileRequestError ? write.error : undefined
  const stale =
    write.error instanceof StaleWrite && write.error.code === 'desk-config-changed'
      ? write.error
      : undefined
  // The decoder's own sentence for the one key this card can be wrong about.
  const problem = (refused?.problems ?? []).find((each) => each.key === 'project.file')
  // Anything else the route may refuse with — a body too large, an unusable
  // store — still has to be said rather than swallowed.
  const otherRefusal =
    stale === undefined && problem === undefined ? (write.error?.message ?? undefined) : undefined

  const save = () => {
    if (digest === undefined) return
    setSaid(undefined)
    // An empty field is a real choice — a desk that configures no default —
    // and is sent as null, which is the value the schema admits.
    const file = draft.trim() === '' ? null : draft.trim()
    write.mutate(
      { file, ifMatch: digest },
      {
        onSuccess: (answer) => {
          // What landed, read back off the disk — not what was sent.
          setDirty(false)
          setSaid(answer.project.file === null ? CLEARED : SAVED)
        }
      }
    )
  }

  return {
    field: (
      <CardField
        label="Default project"
        rule={
          <>
            Used on the next launch without a directory. This launch:{' '}
            {desk?.chassis === undefined ? (
              <span className="quiet">the desk has not said</span>
            ) : (
              <code>{desk.chassis.projectDir}</code>
            )}
          </>
        }
      >
        <Input
          aria-label="Default project"
          value={draft}
          spellCheck={false}
          disabled={write.isPending}
          onChange={(event) => {
            setDirty(true)
            setSaid(undefined)
            setDraft(event.target.value)
          }}
        />
      </CardField>
    ),
    save: (
      <>
        <p className="actions">
          <Button
            variant="primary"
            disabled={digest === undefined || write.isPending}
            onClick={save}
          >
            Save
          </Button>{' '}
          {write.isPending && <span className="quiet">writing…</span>}
          {said !== undefined && !write.isPending && <span className="quiet">{said}</span>}
        </p>
        {digest === undefined && <p className="quiet">{NO_DIGEST}</p>}
        {problem !== undefined && (
          <p className="partial-reason">
            {problem.key}: {problem.reason}
          </p>
        )}
        {stale !== undefined && (
          <AlertPanel
            heading="The configuration changed on disk. Nothing was written."
            detailLabel="digests"
            detail={
              <>
                <span>
                  this page read{' '}
                  <code title={stale.expectedSha256}>sha256 {short(stale.expectedSha256)}</code>
                </span>
                <span>
                  on disk now{' '}
                  <code title={stale.actualSha256}>sha256 {short(stale.actualSha256)}</code>
                </span>
              </>
            }
            actions={
              <Button
                variant="primary"
                disabled={write.isPending}
                onClick={() => {
                  write.reset()
                  void client.refetchQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
                }}
              >
                Reload
              </Button>
            }
          >
            <span>
              What is typed here is still here. Reload reads the file again, so the next Save
              states a digest that is true.
            </span>
          </AlertPanel>
        )}
        {otherRefusal !== undefined && <Alert reason={otherRefusal}>Nothing was written.</Alert>}
      </>
    )
  }
}

function short(value: string): string {
  return value ? `${value.slice(0, 12)}…` : '(no file)'
}
