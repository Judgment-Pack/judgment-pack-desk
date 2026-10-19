import {useQuery} from '@tanstack/react-query'
import {graphDraftsQuery,type GraphDraftDocument} from './drafts'
import {uniquePolicySources} from '../policies/authoringSource'
import {GraphSources} from './GraphSources'
import type {GraphDocument} from '../mcp/types'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {LoadingState} from '../ui/LoadingState'
import {msg} from '../i18n'

export function savedGraphPolicySources(doc:GraphDraftDocument|undefined,id:string,path:string|undefined,content:string|undefined){return content===undefined?[]:uniquePolicySources((doc?.drafts??[]).filter(d=>(d.saved||d.writePending)&&d.id===id&&d.path===path&&d.content===content).flatMap(d=>d.policySources??[]))}
export function SavedGraphSources({id,path,content,document}:{id:string;path?:string;content?:string;document?:GraphDocument}){
 const query=useQuery(graphDraftsQuery())
 return <><QueryStatusNotice id={'graph-sources:'+id} label={msg('Graph sources unavailable')} error={query.error} onRetry={()=>query.refetch()}/>{query.data?.content.drafts.some(d=>d.writePending&&d.id===id&&d.path===path&&d.content===content)&&<p role="status">{msg('Private graph history is awaiting reconciliation with the saved file.')}</p>}{query.isPending?<LoadingState>{msg('Loading sources…')}</LoadingState>:query.data?<GraphSources document={document} policySources={savedGraphPolicySources(query.data.content,id,path,content)}/>:null}</>
}
