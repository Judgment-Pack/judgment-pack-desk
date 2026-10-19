import {useEffect,useState} from 'react'
import {msg} from '../i18n'
import {useEffectiveConfig} from '../config/DeskConfigProvider'
import {loadDocument,matchesPageQuote,type DocumentReference,type VerifiedDocument} from './client'
import {usablePages} from './record'

export type PageCitation={page:number;quote:string}
/** Receipt verification and quote matching are separate. A mismatched quote
 * does not make verified source bytes unavailable. Never retry a deterministic
 * mismatch, or expose bytes when receipt verification failed. */
export function useDocumentSource(reference:DocumentReference,enabled:boolean,citation?:PageCitation){
 const pin=useEffectiveConfig().config.research.gateway
 const [attempt,retry]=useState(0)
 const [state,setState]=useState<{key:string;value?:VerifiedDocument;error?:string;citationProblem?:string}>({key:''})
 const key=JSON.stringify([reference,pin?.authority,pin?.signer.public,citation,attempt])
 useEffect(()=>{
  if(!enabled)return
  const operation=new AbortController()
  setState({key})
  if(!pin){setState({key,error:msg('Configure the gateway in Admin → Document processing to use attached PDFs.')});return}
  void loadDocument(reference,pin,operation.signal).then(value=>{
   if(operation.signal.aborted)return
   let citationProblem:string|undefined
   if(citation){
    if(!reference.pages.includes(citation.page)||!usablePages(value.record).some(page=>page.number===citation.page))citationProblem=msg('The cited page is not among the selected readable pages.')
    else if(!matchesPageQuote(value,citation.page,citation.quote))citationProblem=msg('This quote does not match the cited page.')
   }
   setState({key,value,citationProblem})
  },error=>{if(!operation.signal.aborted)setState({key,error:(error as Error).message})})
  return()=>operation.abort()
 },[key,enabled])
 return {...(enabled&&state.key===key?state:{}),retry:()=>retry(value=>value+1)}
}
