/**
 * The assistant slot as react-query hooks.
 *
 * The key state is a query because it is a fact about this machine that the
 * page reads; the store, the removal and the probe are mutations because each
 * one *does* something — two of them change what is on disk, and the third
 * opens an outbound connection. None of them retries: a retried store is one
 * write becoming two, and a retried probe is a page reporting a latency it
 * measured on an attempt it did not tell anyone about.
 */
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult
} from '@tanstack/react-query'
import { useRef } from 'react'
import {
  probeAssistantEndpoint,
  readAssistantKey,
  removeAssistantKey,
  storeAssistantKey,
  updateAssistantConfig,
  type AssistantConfigWrite,
  type AssistantConfigWritten,
  type AssistantKeyState,
  type ProbeResult
} from './client'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import type { EffectiveConfig } from '../config/deskConfig'

export const ASSISTANT_KEY_QUERY_KEY = ['assistant-key'] as const

/**
 * Whether a key is stored, and enough of it to recognise.
 *
 * `staleTime: Infinity` with no background refetch: the answer changes only
 * when this page changes it, and the two mutations below write the result they
 * were given straight into the cache rather than asking again.
 */
export function useAssistantKey(): UseQueryResult<AssistantKeyState, Error> {
  return useQuery({
    queryKey: ASSISTANT_KEY_QUERY_KEY,
    staleTime: Infinity,
    retry: false,
    queryFn: ({ signal }) => readAssistantKey(signal)
  })
}

/**
 * One store, and a credential that is a **variable of nothing**.
 *
 * `useMutation` retains what it was called with: `variables` stays on the
 * mutation in the cache for as long as its entry lives, which is minutes after
 * it settles. Resetting the observer does not clear that — the entry is the
 * cache's, not the observer's — so a store that took the key as its variable
 * left the plaintext in React Query whether it succeeded or failed. Calling
 * `reset()` looked like a fix and was not; a test that reads the mutation
 * cache is what said so.
 *
 * So the mutation takes no variable at all. The key travels in a ref, which is
 * cleared the instant the request is handed to `fetch`, and nothing in the
 * cache ever holds it. The window in which it exists in this page is the
 * window in which it is being sent, which is the shortest one there is.
 */
export interface StoreAssistantKey {
  /**
   * Send one key. It is dropped as soon as the request has been made.
   *
   * `onStored` is handed the state the chassis answered with — `present`, the
   * fingerprint, and the destination the key is now bound to — and never the
   * key. It exists because a store is what *repairs* a binding the page is
   * reporting as broken, and the row has to stop saying so at the moment the
   * chassis says otherwise.
   */
  submit: (
    key: string,
    handlers?: {
      onError?: (error: Error) => void
      onStored?: (state: AssistantKeyState) => void
    }
  ) => void
  isPending: boolean
}

export function useStoreAssistantKey(): StoreAssistantKey {
  const client = useQueryClient()
  const pending = useRef<string | null>(null)
  const mutation = useMutation<AssistantKeyState, Error, void>({
    mutationFn: async () => {
      const key = pending.current ?? ''
      // Dropped before the promise is awaited, not after it resolves: a
      // request that never comes back must not leave it here.
      pending.current = null
      return storeAssistantKey(key)
    },
    // The chassis answers with the state it now holds, so the cache is set
    // from that rather than invalidated and re-read. Setting it from what was
    // *sent* would be the page reporting its own request as an outcome.
    onSuccess: (state) => client.setQueryData(ASSISTANT_KEY_QUERY_KEY, state)
  })
  return {
    isPending: mutation.isPending,
    submit: (key, handlers) => {
      pending.current = key
      mutation.mutate(undefined, {
        onError: handlers?.onError,
        onSuccess: (state) => handlers?.onStored?.(state),
        onSettled: () => {
          pending.current = null
        }
      })
    }
  }
}

export function useRemoveAssistantKey(): UseMutationResult<AssistantKeyState, Error, void> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: () => removeAssistantKey(),
    onSuccess: (state) => client.setQueryData(ASSISTANT_KEY_QUERY_KEY, state)
  })
}

export function useProbeAssistant(): UseMutationResult<ProbeResult, Error, void> {
  return useMutation({ mutationFn: () => probeAssistantEndpoint() })
}

/**
 * What the cached configuration becomes once a write has landed.
 *
 * **The members the answer actually carries, over the value already there.**
 * The write answers with the `assistant` slot as the chassis read it back off
 * the disk, the file's path, and the new digest — so those are set, and every
 * other part of the effective configuration is carried across untouched rather
 * than assembled. That distinction is the whole of it: the objection to
 * writing this cache was that a write answers about one slot while the cache
 * holds two files layered, and it is answered by not touching the layers.
 *
 * `undefined` where nothing is cached yet, because there is nothing to carry
 * across and the read that follows is what fills it.
 *
 * **`problems: []` is told, not assumed**: the chassis decodes the file it
 * composed before any of it reaches the disk, so a write that landed is a file
 * its own reader accepted. `note` and `readFailure` are dropped for the same
 * reason — they described a file that was absent or unread, and it is neither.
 */
export function configAfterWrite(
  previous: EffectiveConfig | undefined,
  written: AssistantConfigWritten
): EffectiveConfig | undefined {
  if (previous === undefined) return undefined
  return {
    ...previous,
    config: { ...previous.config, assistant: written.assistant },
    // `assistant` may only come from the desk-level file, and there now is one.
    sources: { ...previous.sources, assistant: 'desk file' },
    desk: { path: written.path, present: true, problems: [], sha256: written.sha256 }
  }
}

/**
 * Write the desk-level `assistant` object, and re-read the configuration.
 *
 * **Set from the answer *and* invalidated**, in that order, and the order is
 * the point. The tab's status line, Describe it and the key row all read the
 * slot through this cache; leaving them to a second `GET` meant that a write
 * which landed while the read that follows it failed — or simply hung — left
 * every one of those surfaces describing the endpoint that had just been
 * replaced, under a form that said "Saved". The answer already carries what
 * they need: the decoded slot, read back off the disk rather than echoed, and
 * the digest the next write states. So it is written in, and the re-read still
 * happens for the parts a write cannot speak about.
 *
 * **The key query is invalidated too.** The binding is the desk's verdict and
 * a write can move it in either direction; `keyRebindRequired` covers the
 * moment for the direction that matters, and the read is what settles it.
 *
 * No retry, for the reason none of the others has one: a retried write is one
 * conditional commit becoming two, and the second would carry an `ifMatch` the
 * first has already made stale.
 */
export function useUpdateAssistantConfig(): UseMutationResult<
  AssistantConfigWritten,
  Error,
  AssistantConfigWrite
> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: AssistantConfigWrite) => updateAssistantConfig(input),
    retry: false,
    // On success only: a refused write changed nothing, and re-reading after
    // one would be this page telling itself that something happened.
    onSuccess: (written) => {
      client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, (previous) =>
        configAfterWrite(previous, written)
      )
      void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
      void client.invalidateQueries({ queryKey: ASSISTANT_KEY_QUERY_KEY })
    }
  })
}
