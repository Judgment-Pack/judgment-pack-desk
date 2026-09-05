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

export function useStoreAssistantKey(): UseMutationResult<AssistantKeyState, Error, string> {
  const client = useQueryClient()
  return useMutation({
    mutationFn: storeAssistantKey,
    // The chassis answers with the state it now holds, so the cache is set
    // from that rather than invalidated and re-read. Setting it from what was
    // *sent* would be the page reporting its own request as an outcome.
    onSuccess: (state) => client.setQueryData(ASSISTANT_KEY_QUERY_KEY, state)
  })
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
