import { sourceMessage } from '../i18n/source'
import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
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
 * The row explains that changes affect the next startup, not the current
 * desk. Its immediate action aligns with the value and stacks on narrow views.
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

const SET = sourceMessage('Saved. The next launch without a directory opens this project.')
const CLEARED = sourceMessage('Saved. This desk configures no default project.')

export function useDefaultProject(): { field: ReactNode; save: ReactNode } {
  useLocale()
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
        label={msg("Startup project")}
        action={isThisProject ? (
          <Button variant="secondary" disabled={blocked} onClick={() => commit(null, CLEARED)}>{msg("Clear startup project")}</Button>
        ) : (
          <Button
            variant="secondary"
            disabled={blocked}
            onClick={() => commit(chassis?.projectFile ?? null, SET)}
          >{msg("Open this project at startup")}</Button>
        )}
        rule={msg('Changes apply to the next launch without a project folder. The current desk stays open.')}
      >
        {configured === null ? (
          msg("None")
        ) : isThisProject ? (
          msg("this project")
        ) : (
          <code>{configured}</code>
        )}{' '}
        {write.isPending && <span className="quiet">{msg("Saving…")}</span>}
        {said !== undefined && !write.isPending && <span className="quiet">{msg(said)}</span>}
      </CardField>
    ),
    save: (
      <>
        {digest === undefined && <p className="quiet">{msg(NO_DIGEST)}</p>}
        {digest !== undefined && chassis === undefined && <p className="quiet">{msg(NOT_SAID)}</p>}
        {problem !== undefined && (
          <p className="partial-reason">
            {problem.key}: {systemMessage(problem.reason)}
          </p>
        )}
        {stale !== undefined && (
          <AlertPanel
            heading={msg("The configuration changed on disk. Nothing was written.")}
            detailLabel={msg("digests")}
            detail={
              <>
                <span><Message text={"this page read<0/><1/>"} slots={[' ', <Digest value={stale.expectedSha256} />]} /></span>
                <span><Message text={"on disk now<0/><1/>"} slots={[' ', <Digest value={stale.actualSha256} />]} /></span>
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
              >{msg("Reload")}</Button>
            }
          >
            <span>{msg("Reload reads the file again, so the next save states a true digest.")}</span>
          </AlertPanel>
        )}
        {otherRefusal !== undefined && <Alert reason={otherRefusal}>{msg("Nothing was written.")}</Alert>}
      </>
    )
  }
}
