import {privateQuery} from '../data/privateQuery'
import {queryOptions} from '@tanstack/react-query'
import {dialectAPI,type PreparationReply,type PreparationSummary} from './client'

// Dialects live in private storage, outside the project file watcher. They
// cannot inherit the document cache's infinite freshness. Refresh on entry and
// focus; poll the visible collection for saves made by another chat/window.
// Failed refreshes retain the last successful data in the shared query cache.

export const dialectListQuery=()=>queryOptions({
 queryKey:['input-preparations'],
 queryFn:({signal})=>dialectAPI<{items:PreparationSummary[]}>('','GET',undefined,undefined,signal),
 ...privateQuery,refetchInterval:5000,
})
export const dialectQuery=(id:string)=>queryOptions({
 queryKey:['input-preparations',id],
 queryFn:({signal})=>dialectAPI<PreparationReply>('/'+id,'GET',undefined,undefined,signal),
 ...privateQuery,
})
