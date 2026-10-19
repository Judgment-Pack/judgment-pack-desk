import {policySourceKey} from '../../policies/authoringSource'
import {LoadingState} from '../../ui/LoadingState'
import { useMemo } from 'react'
import type { ChatAttachment } from '../../chat/store'
import type { SourceRecord } from '../../research/ledger'
import { useChats } from '../../chat/ChatProvider'
import { useFileContent, useFileListing } from '../../files/queries'
import { msg } from '../../i18n'
import { DraftSources } from '../../research/ui/DraftSources'
import { QueryStatusNotice } from '../../shell/QueryStatusNotice'
import { readSourceHandoff, restoreSourceHandoff, sourceHandoff } from './sourceHandoff'

export function SavedPackSources({owner, path, matrixPath, document}: {owner:string; path?:string; matrixPath?:string; document:unknown}) {
  const chats = useChats()
  const listing = useFileListing()
  const researchPath = matrixPath?.endsWith('.matrix.json') ? matrixPath.replace(/\.matrix\.json$/, '.research.json') : path?.endsWith('.pack.json') ? path.replace(/\.pack\.json$/, '.research.json') : undefined
  const file = useFileContent(listing.data?.files.some(file => file.path === researchPath) ? researchPath : undefined)
  const drafts = chats.packDrafts.filter(draft => draft.finalized?.id === owner && (!path || draft.finalized.path === path))
  const retained = useMemo(() => {
    let error: unknown
    const inputs = drafts.map(draft => sourceHandoff(draft))
    if (file.data) {
      try {
        const record = JSON.parse(file.data.content)
        if (record.authoringSources !== undefined) inputs.unshift(record.authoringSources)
      } catch (problem) { error = problem }
    }
    const documents = new Map<string, ChatAttachment>(), files = new Map<string, {name:string;text:string}>(), sources = new Map<string, SourceRecord>()
    for (const input of inputs) {
      try {
        const saved = readSourceHandoff(input)
        for (const file of saved.documents) documents.set(JSON.stringify([file.id,file.document?.digest,file.document?.pages]),file)
        for (const file of saved.files) files.set(JSON.stringify([file.name,file.text]),file)
        for (const source of restoreSourceHandoff(saved.retrievals)) sources.set(JSON.stringify([source.session,source.id,source.request]),source)
      } catch (problem) { error = problem }
    }
    const policySources=[...new Map(drafts.flatMap(draft=>draft.policySources??[]).map(source=>[policySourceKey(source),source])).values()]
    return {policySources,documents:[...documents.values()], files:[...files.values()], sources:[...sources.values()], error}
  }, [file.data, chats.packDrafts, owner, path])
  const loading = Boolean(chats.store && !chats.ready) || listing.isPending || Boolean(listing.data?.files.some(entry=>entry.path===researchPath) && file.isPending)
  return <>
    <QueryStatusNotice id={'pack-sources:'+owner} label={msg('Pack sources unavailable')} error={retained.error || file.error || listing.error} onRetry={()=>{void listing.refetch();if(file.data||file.error)void file.refetch()}}/>
    {loading && !retained.documents.length && !retained.files.length && !retained.sources.length && !retained.policySources.length ? <LoadingState>{msg('Loading sources…')}</LoadingState> : <DraftSources document={document} {...retained}/>}
  </>
}
