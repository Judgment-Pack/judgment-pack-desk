import { useQuery, type QueryClient } from '@tanstack/react-query'
import type { EvaluationRun } from '../mcp/types'

export interface PackRunSnapshot { id: string; packId: string; packBytes?: string; run: EvaluationRun }
const key = (packId?: string) => ['pack-explanation', packId] as const
/** Only one explicitly requested explanation per pack, in memory, never browser storage. */
export function publishPackRun(client: QueryClient, snapshot: PackRunSnapshot) { client.setQueryData(key(snapshot.packId), snapshot) }
export function usePackRun(packId?: string) {
  return useQuery<PackRunSnapshot | null>({ queryKey: key(packId), queryFn: () => null, enabled: false, staleTime: Infinity })
}
export function traceMatches(snapshot: PackRunSnapshot | null | undefined, packId: string | undefined, bytes: string | undefined): boolean {
  return snapshot !== null && snapshot !== undefined && snapshot.packId === packId && snapshot.packBytes !== undefined && snapshot.packBytes === bytes
}
