/**
 * The default project, written to the desk-level file.
 *
 * **The same conditional commit the assistant slot is written under**, and the
 * same route: `PUT /api/desk-config` takes `{assistant?, project?, ifMatch}`,
 * replaces the members it was sent, and carries every other member of the file
 * across by its own bytes. So this sends `project` alone — it has no opinion
 * about the assistant slot and must not overwrite one by restating it — and
 * states the digest its read carried.
 *
 * There is no `override`, here as there. This file names the endpoint a
 * credential is presented to and now the project a desk opens, and "write
 * anyway" is not a choice a page should be able to make about either.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query'
import { answer, chassisUrl } from '../files/client'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import type { EffectiveConfig } from '../config/deskConfig'

/** What a default-project write asks for. */
export interface ProjectDefaultWrite {
  /** The absolute path, or null for a desk that configures no default. */
  file: string | null
  /** The digest of the bytes this page last read. */
  ifMatch: string
}

/** What the chassis answers a desk-level write with, for this member. */
export interface ProjectDefaultWritten {
  path: string
  sha256: string
  project: { file: string | null }
  created: boolean
}

export async function updateProjectDefault(
  input: ProjectDefaultWrite
): Promise<ProjectDefaultWritten> {
  return answer<ProjectDefaultWritten>(
    await fetch(chassisUrl('/api/desk-config'), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      // **`project` and nothing else.** A body that also restated `assistant`
      // would make this card an author of a member it never read from the
      // form, and two cards writing one file would race over each other's.
      body: JSON.stringify({ project: { file: input.file }, ifMatch: input.ifMatch })
    })
  )
}

/**
 * What the cached configuration becomes once a default-project write lands.
 *
 * **The members the answer actually carries, over the value already there.**
 * The write answers with the `project` slot as the chassis read it back off the
 * disk and the new digest, so those are set and every other part of the
 * effective configuration is carried across untouched — the same rule
 * `configAfterWrite` follows for the assistant slot, and for the same reason: a
 * write answers about one member while the cache holds two files layered.
 */
export function configAfterProjectWrite(
  previous: EffectiveConfig | undefined,
  written: ProjectDefaultWritten
): EffectiveConfig | undefined {
  if (previous === undefined) return undefined
  return {
    ...previous,
    config: { ...previous.config, project: written.project },
    // `project` may only come from the desk-level file, and there now is one.
    sources: { ...previous.sources, project: 'desk file' },
    desk: {
      ...previous.desk,
      path: written.path,
      present: true,
      problems: [],
      sha256: written.sha256
    }
  }
}

/**
 * Write the default project, and re-read the configuration.
 *
 * **Set from the answer *and* invalidated**, in that order, for the reason
 * `useUpdateAssistantConfig` gives: the answer already carries what the page
 * needs — the decoded slot, read back off the disk rather than echoed, and the
 * digest the next write states — so a read that hangs afterwards does not leave
 * the card describing what was just replaced under a form that said "Saved".
 *
 * No retry: a retried conditional commit is one write becoming two, and the
 * second would carry an `ifMatch` the first has already made stale.
 */
export function useUpdateProjectDefault(): UseMutationResult<
  ProjectDefaultWritten,
  Error,
  ProjectDefaultWrite
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: ProjectDefaultWrite) => updateProjectDefault(input),
    retry: false,
    // On success only: a refused write changed nothing, and re-reading after
    // one would be this page telling itself that something happened.
    onSuccess: (written) => {
      client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, (previous) =>
        configAfterProjectWrite(previous, written)
      )
      void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
    }
  })
}
