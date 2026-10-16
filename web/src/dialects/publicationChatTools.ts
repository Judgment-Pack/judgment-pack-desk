import type {HostTool} from '../assistant/engine'
import {toolInputFailure} from '../assistant/failures'
import {dialectAPI,type PreparationReply} from './client'
import {validDialectId,validDialectRevision,type DialectChatAction} from './chatActions'
import {validPublicationProposal} from './publicationProposal'
import type {DialectPublicationAnswer} from './DialectPublication'
const schema=(properties:Record<string,unknown>,required=Object.keys(properties))=>({type:'object',properties,required,additionalProperties:false})
const id={type:'string',pattern:'^prep_[a-f0-9]{32}$'},variant={type:'string',enum:['asserted','generated']}
const result=(value:unknown)=>({content:[{type:'text' as const,text:JSON.stringify(value)}]})
export function publicationChatTools(options:{retainAction:(action:DialectChatAction,signal:AbortSignal)=>Promise<void>;afterTurnId:()=>string|undefined}):HostTool[]{
 const seen=new Map<string,string>()
 return [
 {name:'read_dialect_publication',delegation:'read',description:'Read the saved Dialect mapping revision and latest immutable publication before proposing a version. Imported records use asserted; human-reviewed research uses generated. Reads only. Publishing never changes existing job pins.',inputSchema:schema({id,variant}),execute:async(args,signal)=>{
  if(Object.keys(args).some(k=>!['id','variant'].includes(k))||!validDialectId(args.id)||!['asserted','generated'].includes(String(args.variant)))throw toolInputFailure('Choose a saved Dialect and record kind.')
  const [saved,history]=await Promise.all([dialectAPI<PreparationReply>('/'+args.id,'GET',undefined,undefined,signal),dialectAPI<{items:DialectPublicationAnswer[]}>(`/${args.id}/publications?variant=${args.variant}`,'GET',undefined,undefined,signal)])
  signal.throwIfAborted();seen.set(args.id+':'+args.variant,saved.sha256)
  const latest=history.items[0]
  return result({id:args.id,name:saved.document.name,revision:saved.sha256,recipeVersion:saved.document.recipeVersion,mapping:saved.document.recipe.qualification?.mapping??null,latest:latest?{version:latest.publication.documents[0]?.version,digest:latest.digest,document:latest.document}:null,guidance:'Use Runtime get_artifact_version_options for version choices. Choose and explain initial/patch/minor/major impact; ask if compatibility is uncertain. propose_dialect_publication checks and retains a proposal only. Ask me requires Publish version; Auto requires a separate one-publication allowance. Research permission never authorizes publication. Existing jobs retain their pins.'})
 }},
 {name:'propose_dialect_publication',description:'Check and retain one exact saved Dialect mapping version for publication review. Read read_dialect_publication first. Does not publish, change recipes, or upgrade jobs. Explicit impact rationale and a Runtime-valid version are required.',inputSchema:schema({id,variant,revision:{type:'string',pattern:'^[a-f0-9]{64}$'},version:{type:'string',minLength:1,maxLength:256},impact:schema({classification:{type:'string',enum:['initial','patch','minor','major']},rationale:{type:'string',minLength:1,maxLength:4000}})}),execute:async(args,signal)=>{
  if(Object.keys(args).some(k=>!['id','variant','revision','version','impact'].includes(k))||!validDialectId(args.id)||!validDialectRevision(args.revision)||seen.get(args.id+':'+args.variant)!==args.revision)throw toolInputFailure('Read the current Dialect publication before proposing a version.')
  const saved=await dialectAPI<PreparationReply>('/'+args.id,'GET',undefined,undefined,signal)
  if(saved.sha256!==args.revision)throw toolInputFailure('The dialect changed. Read its current publication again.')
  const answer=await dialectAPI<DialectPublicationAnswer>('/'+args.id+'/publications','POST',{action:'check',revision:args.revision,variant:args.variant,version:args.version,impact:args.impact},undefined,signal)
  signal.throwIfAborted()
  if(answer.saved)return result({status:'already-published',version:answer.publication.documents[0]?.version,digest:answer.digest})
  const publication={variant:args.variant,version:args.version,impact:args.impact,answer}
  if(!validPublicationProposal(publication))throw toolInputFailure('The publication check was incomplete. Review the saved recipe and compatibility impact.')
  const action:DialectChatAction={version:1,id:crypto.randomUUID(),createdAt:new Date().toISOString(),afterTurnId:options.afterTurnId(),inputId:args.id,name:saved.document.name,revision:saved.sha256,recipeVersion:saved.document.recipeVersion,kind:'publish-dialect',publication}
  await options.retainAction(action,signal)
  return result({status:'review-required',actionId:action.id,version:publication.version,digest:answer.digest,message:'Review this version in chat. No publication or job upgrade has been saved.'})
 }}]
}
