import { useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useMcp } from '../../mcp/McpProvider'
import type { GraphSuite, GraphSuiteEntry } from '../../mcp/types'

/** Collection metadata observes completed runs, without fetching a suite.
 * Both scoped/all-flow and traced/untraced requests can hold the latest result. */
export function useLatestFlowResult(graphId: string): GraphSuiteEntry | null {
  const client = useQueryClient()
  const { connectionEpoch } = useMcp()
  const cache = client.getQueryCache()
  return useSyncExternalStore(listener => cache.subscribe(listener), () => {
    let latest: GraphSuiteEntry | null = null
    let updated = 0
    for (const query of cache.findAll({ queryKey: ['experimental_test_graphs'] })) {
      if (query.queryKey[3] !== connectionEpoch || query.state.dataUpdatedAt <= updated) continue
      const data = client.getQueryData<GraphSuite>(query.queryKey)
      const result = data?.graphs?.find(entry => entry.id === graphId)
      if (result) { latest = result; updated = query.state.dataUpdatedAt }
    }
    return latest
  })
}
