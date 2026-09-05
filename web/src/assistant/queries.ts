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
  type AssistantKeyState,
  type ProbeResult
} from './client'

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
  /** Send one key. It is dropped as soon as the request has been made. */
  submit: (key: string, handlers?: { onError?: (error: Error) => void }) => void
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
