import {useQuery, useQueryClient} from '@tanstack/react-query'
import {activeDeskId} from '../desks/scope'
import {privateQuery} from '../data/privateQuery'
import {deskFetch} from '../files/client'
import {readStorageBudgetStatus, retainStorageSnapshot, storageLineOff, type StorageBudgetStatus} from './storageBudgetModel'

export const storageBudgetStatusKey = ['storage-budget-status', activeDeskId] as const
/** Only shell health owns a timer/focus/reconnect read; all panels observe its cache. */
export function useStorageBudgetStatus(owner = false) {
  const client = useQueryClient()
  return useQuery({
    ...privateQuery,
    queryKey: storageBudgetStatusKey,
    enabled: owner,
    refetchInterval: owner ? privateQuery.refetchInterval : false,
    refetchOnMount: owner ? privateQuery.refetchOnMount : false,
    refetchOnWindowFocus: owner,
    refetchOnReconnect: owner,
    queryFn: async ({signal}) => retainStorageSnapshot(
      await readStorageBudgetStatus(await deskFetch('/api/storage/status', {signal})),
      client.getQueryData<StorageBudgetStatus>(storageBudgetStatusKey),
    ),
  })
}
/** Whether the storage line is off, as the shell's status read last said. Unknown until that read lands. */
export function useStorageLineOff(): boolean {
  return storageLineOff(useStorageBudgetStatus().data)
}
