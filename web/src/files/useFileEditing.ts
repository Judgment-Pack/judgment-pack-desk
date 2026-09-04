/**
 * One file, open: the base revision, the save, and the proof the save left
 * behind.
 *
 * Lifted out of `AuthorView.FileEditor` unchanged, because the pack editor
 * needs exactly this and two copies of a concurrency story is two stories. The
 * three rules it holds are the ones a plain editor gets wrong, and each is
 * here rather than at a call site so neither editor can hold a different one:
 *
 * - **The base moves only where the viewer acts** — an initial load, an
 *   explicit reload, a successful save. Never on a watcher refetch. The desk
 *   invalidates every query when the chassis sees a file change, and a base
 *   derived from that live query would silently rebase onto bytes nobody saw,
 *   so Save would overwrite them without the 409 that exists to prevent
 *   exactly that.
 * - **Reload is a direct read, not `refetch()`.** The watcher's broad
 *   `cancelQueries()` makes `refetch` report success from cache when it
 *   cancels the request in flight, so its success is not proof that anything
 *   was fetched — and installing cached bytes as the new base is a reload that
 *   replaces an edit with what it was already showing.
 * - **The save is judged against what it sent.** The submitted snapshot is
 *   captured with the request, and the chassis' read-back is compared to
 *   *that* and never to the live buffer, so typing after a save cannot turn a
 *   true "verified" into a false "does not match".
 *
 * The buffer itself is the caller's. This module holds the revision and the
 * write; `packs/edit/useDocumentBuffer.ts` and `AuthorView`'s own `useState`
 * hold the bytes.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'
import { readFile, type FileContent, type FileEntry, type FileListing } from './client'
import { useWriteFile } from './queries'

/** What the last save produced, kept apart from the live buffer. */
export interface SaveOutcome {
  /** The bytes that were submitted, captured at the moment of the request. */
  submitted: string
  /** What the chassis read back off the disk afterwards. */
  landed: FileContent
}

export interface FileEditing {
  /** The write mutation, for `isPending` and `error`. */
  write: ReturnType<typeof useWriteFile>
  outcome: SaveOutcome | undefined
  /**
   * The failed reload, and **which file it was for**.
   *
   * A read takes as long as it takes, and the page can be about another pack by
   * the time it fails. "Could not reload <the path on screen now>" was then a
   * sentence about a file nothing had tried to read.
   */
  reloadError: { path: string; error: Error } | undefined
  /** The read-back is byte for byte what was sent. */
  verified: boolean
  /**
   * Save `content` against `baseSha256`.
   *
   * `onSaved` is handed the read-back, so the caller can move its own base
   * onto the revision that landed.
   */
  save: (input: {
    path: string
    content: string
    baseSha256: string
    override?: boolean
    createParents?: boolean
    onSaved?: (landed: FileContent) => void
    /** However it ended — so a caller holding a single-flight latch can let go. */
    onSettled?: () => void
  }) => void
  /** Read the file again and hand back what is on disk now. */
  reload: (path: string, onLoaded: (fresh: FileContent) => void) => void
  /** Forget the last attempt's verdict, which a discard does. */
  reset: () => void
}

export function useFileEditing(): FileEditing {
  const write = useWriteFile()
  const queryClient = useQueryClient()
  const [outcome, setOutcome] = useState<SaveOutcome | undefined>(undefined)
  const [reloadError, setReloadError] = useState<{ path: string; error: Error } | undefined>(
    undefined
  )
  // Only the last reload asked for counts. An earlier one resolving afterwards
  // is answering a question that has been replaced.
  const reloads = useRef(0)

  const reset = useCallback(() => {
    setOutcome(undefined)
    setReloadError(undefined)
    write.reset()
    // `write` is a stable mutation object from react-query, and depending on
    // it would rebuild this callback on every state change of the mutation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const reload = useCallback(
    (path: string, onLoaded: (fresh: FileContent) => void) => {
      const ticket = (reloads.current += 1)
      setReloadError(undefined)
      // **The conflict stands until the read lands.** Clearing it first left a
      // failed reload with nothing on screen at all: no stale-write notice, no
      // error, and a Save button that would 409 again — the page had forgotten
      // the one fact the author needed. The verdict and the conflict are
      // cleared on success, which is where they stop being true.
      // A direct read, not a refetch: see the module doc. Only this request's
      // own answer counts.
      void readFile(path)
        .then((fresh) => {
          if (ticket !== reloads.current) return
          write.reset()
          setOutcome(undefined)
          onLoaded(fresh)
          queryClient.setQueryData(['desk-file', path], fresh)
        })
        .catch((cause: unknown) => {
          if (ticket !== reloads.current) return
          setReloadError({
            path,
            error: cause instanceof Error ? cause : new Error(String(cause))
          })
        })
      // eslint-disable-next-line react-hooks/exhaustive-deps
    },
    [queryClient]
  )

  const save = useCallback(
    (input: {
      path: string
      content: string
      baseSha256: string
      override?: boolean
      createParents?: boolean
      onSaved?: (landed: FileContent) => void
      onSettled?: () => void
    }) => {
      // A previous verdict does not survive into a new attempt: leaving
      // "Saved, and verified" on screen while the next save is pending or
      // failing states something about bytes that are no longer the question.
      setOutcome(undefined)
      // Captured here, with the request. Everything about verifying this save
      // is judged against it and never against the buffer, which the viewer is
      // free to keep typing into.
      const submitted = input.content
      // When this save was issued, measured against the file query's own clock.
      const startedAt =
        queryClient.getQueryState(['desk-file', input.path])?.dataUpdatedAt ?? 0
      // **`mutateAsync`, not `mutate`, for the settlement.** A per-mutation
      // `onSettled` is delivered through the observer, and `write.reset()` —
      // which the route calls when the path changes — detaches it: a save in
      // flight across that never settled, so a caller holding a single-flight
      // latch held it for ever and every later Save returned silently. The
      // promise resolves either way, whatever happens to the observer.
      void write
        .mutateAsync(
          {
            path: input.path,
            content: submitted,
            baseSha256: input.baseSha256,
            override: input.override,
            createParents: input.createParents
          },
          {
          onSuccess: (landed) => {
            setOutcome({ submitted, landed })
            input.onSaved?.(landed)
            // The read-back is authoritative about the bytes this save wrote,
            // and *not* about anything that happened afterwards. A watcher
            // refetch that completed while this PUT was in flight is newer
            // than this answer, and installing over it would replace a fresher
            // read and clear the invalidation that fetched it.
            const state = queryClient.getQueryState(['desk-file', input.path])
            if (state !== undefined && state.dataUpdatedAt > startedAt) return
            // The read-back is the authority on what is now on disk, so the
            // caches are told rather than left to disagree with it. Without
            // this the page can say "Saved, and verified" beside a "changed on
            // disk" warning derived from the pre-save cache — both from the
            // same save.
            queryClient.setQueryData(['desk-file', input.path], landed)
            queryClient.setQueryData(['desk-files'], (previous: FileListing | undefined) =>
              previous === undefined
                ? previous
                : {
                    ...previous,
                    files: upsertListed(previous.files, {
                      path: landed.path,
                      bytes: landed.bytes,
                      sha256: landed.sha256
                    })
                  }
            )
          }
          }
        )
        // The mutation's own error is rendered from `write.error`; this catch
        // exists so a rejected promise is not an unhandled one.
        .catch(() => {})
        .finally(() => input.onSettled?.())
      // eslint-disable-next-line react-hooks/exhaustive-deps
    },
    [queryClient]
  )

  // The proof, not the assumption: the chassis read the file back off the disk
  // after the rename, and this compares that to the bytes that were sent.
  const verified = outcome !== undefined && outcome.landed.content === outcome.submitted

  return { write, outcome, reloadError, verified, save, reload, reset }
}

/** The listing with one entry replaced, or added where it was not there. */
export function upsertListed(files: FileEntry[], entry: FileEntry): FileEntry[] {
  const without = files.filter((file) => file.path !== entry.path)
  return [...without, entry].sort((a, b) => a.path.localeCompare(b.path))
}
