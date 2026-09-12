/**
 * The Project card's one control: whether this desk opens **this** project
 * when it is launched without a directory.
 *
 * # Why it is a button and not a path field
 *
 * It was a field. The field was an expansion of authority, and the shape is
 * the fix rather than a validation on top of one. This desk pins one project
 * root at startup and serves the file API through it; `project.file` chooses
 * the root of the **next** launch. So page code that could write any path
 * could hand its successor a root outside the authority the page itself had —
 * `{"project":{"file":"/jpack-desk.json"}}` was enough to pin `/` on the next
 * argument-less start, and the file API would then serve the host.
 *
 * The chassis refuses any value but this project's own file or null, so a
 * free-text field could only offer a refusal for everything else. What is left
 * is the two things a page may actually do: **nominate the project it is
 * running in**, which grants nothing it does not already have, and **withdraw
 * a default**, which takes authority away. A different default is an
 * operator's to write, in the desk-level file, with a shell.
 *
 * # What the line under it says
 *
 * Three facts, because the card's Location, Status and Content are about the
 * *project* file while this control writes the *desk-level* one. The line
 * names the write target — from the chassis' own answer, never composed — what
 * the value is for, and which directory this launch is actually on, because
 * the root is pinned per process and a save moves the next launch and not this
 * one.
 *
 * # Why the button sits on the row and is not primary
 *
 * It applies immediately to this one value, so it is a secondary row action.
 * The label and value align with the rows above; a separate action column
 * places the button at the right edge. The explanation names the desk-level
 * file it writes and stays below the value. Narrow containers stack the row.
 */
import { Digest } from '../ui/Digest'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { FileRequestError, StaleWrite } from '../files/client'
import { Alert } from '../ui/Alert'
import { AlertPanel } from '../ui/AlertPanel'
import { Button } from '../ui/Button'
import { CardField } from './SourceCard'
import { useUpdateProjectDefault } from './projectDefault'

const NO_DIGEST =
  'This desk has not read its own configuration file, and a write states the bytes it replaces.'

const NOT_SAID = 'This desk has not said where its own configuration file is.'

const SET = 'Saved. The next launch without a directory opens this project.'
const CLEARED = 'Saved. This desk configures no default project.'

export function useDefaultProject(): { field: ReactNode; save: ReactNode } {
  const { config, desk } = useEffectiveConfig()
  const client = useQueryClient()
  const write = useUpdateProjectDefault()
  const [said, setSaid] = useState<string | undefined>(undefined)

  const digest = desk?.sha256
  const chassis = desk?.chassis
  const configured = config.project.file
  // **This project, as the chassis spells it.** The comparison is against the
  // value this desk handed the page, because that is the value the chassis
  // will accept; anything this page normalised would be a second spelling rule
  // for a member that is not the page's to choose.
  const isThisProject = chassis !== undefined && configured === chassis.projectFile

  const refused = write.error instanceof FileRequestError ? write.error : undefined
  const stale =
    write.error instanceof StaleWrite && write.error.code === 'desk-config-changed'
      ? write.error
      : undefined
  const problem = (refused?.problems ?? []).find((each) => each.key === 'project.file')
  const otherRefusal =
    stale === undefined && problem === undefined ? (write.error?.message ?? undefined) : undefined

  const commit = (file: string | null, says: string) => {
    if (digest === undefined) return
    setSaid(undefined)
    write.mutate({ file, ifMatch: digest }, { onSuccess: () => setSaid(says) })
  }

  const blocked = digest === undefined || chassis === undefined || write.isPending

  return {
    field: (
      <CardField
        label="Default project"
        action={isThisProject ? (
          <Button variant="secondary" disabled={blocked} onClick={() => commit(null, CLEARED)}>
            Clear the default
          </Button>
        ) : (
          <Button
            variant="secondary"
            disabled={blocked}
            onClick={() => commit(chassis?.projectFile ?? null, SET)}
          >
            Use this project as the default
          </Button>
        )}
        rule={
          <>
            Written to{' '}
            {desk === undefined ? (
              <span className="quiet">a file this desk has not named</span>
            ) : (
              <code>{desk.path}</code>
            )}
            , used on the next launch without a directory. This launch:{' '}
            {chassis === undefined ? (
              <span className="quiet">the desk has not said</span>
            ) : (
              <code>{chassis.projectDir}</code>
            )}
          </>
        }
      >
        {configured === null ? (
          'None'
        ) : isThisProject ? (
          'this project'
        ) : (
          <code>{configured}</code>
        )}{' '}
        {write.isPending && <span className="quiet">writing…</span>}
        {said !== undefined && !write.isPending && <span className="quiet">{said}</span>}
      </CardField>
    ),
    save: (
      <>
        {digest === undefined && <p className="quiet">{NO_DIGEST}</p>}
        {digest !== undefined && chassis === undefined && <p className="quiet">{NOT_SAID}</p>}
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
                  <Digest value={stale.expectedSha256} />
                </span>
                <span>
                  on disk now{' '}
                  <Digest value={stale.actualSha256} />
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
            <span>Reload reads the file again, so the next save states a true digest.</span>
          </AlertPanel>
        )}
        {otherRefusal !== undefined && <Alert reason={otherRefusal}>Nothing was written.</Alert>}
      </>
    )
  }
}
