import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {useMcp} from '../mcp/McpProvider'
import {LoadingState} from '../ui/LoadingState'
import {Button} from '../ui/Button'
import {Menu} from '../ui/Menu'
import {IconHistory} from '../shell/icons'
import {msg,useLocale} from '../i18n'

type Choices=Record<'patch'|'minor'|'major',string>
/** Transport shape validation only; version arithmetic belongs to Runtime. */
export function readVersionOptions(value:unknown,base:string):Choices {
 const r=value as {status?:unknown;scope?:unknown;base?:unknown;choices?:Partial<Choices>;compatibilityChecked?:unknown}|null
 if(!r || r.status!=='available' || r.scope!=='version-arithmetic' || r.base!==base || r.compatibilityChecked!==false || !r.choices || ['patch','minor','major'].some(key=>typeof r.choices?.[key as keyof Choices]!=='string' || !r.choices[key as keyof Choices] || r.choices[key as keyof Choices]!.length>256))throw Error('Invalid artifact version options response')
 return r.choices as Choices
}
/** Pure authoring assistance: selection edits the caller's draft, never saves it.
 * Publication admission and job-usage policy are separate server responsibilities. */
export function ArtifactVersionOptions({base,onSelect,disabled=false}:{base?:string;onSelect:(version:string)=>void;disabled?:boolean}) {
 useLocale()
 const [open,setOpen]=useState(false)
 const {client,status,known,artifactVersionOptionsSupported,connectionEpoch}=useMcp()
 const supported=known && artifactVersionOptionsSupported && status==='ready' && !!client && !!base
 const options=useQuery({queryKey:['artifact-version-options',connectionEpoch,base],enabled:!!supported&&open,staleTime:Infinity,retry:false,queryFn:async({signal})=>{
  const result=await client!.callTool({name:'get_artifact_version_options',arguments:{base}},undefined,{signal})
  if(result.isError)throw Error('Artifact version options unavailable')
  return readVersionOptions(result.structuredContent,base!)
 }})
 if(!supported)return null
 return <span><Menu.Root open={open} onOpenChange={setOpen}>
  <Menu.Trigger asChild><Button size="small" variant="quiet" disabled={disabled}>{msg('Choose next version')}</Button></Menu.Trigger>
  <Menu.Portal><Menu.Content align="start" aria-label={msg('Version options')}>
   <Menu.Label>{msg('Saved version {{version}}',{version:base})}</Menu.Label>
   {options.data && (['patch','minor','major'] as const).map(part=><Menu.Item key={part} icon={<IconHistory/>} disabled={disabled} onSelect={()=>onSelect(options.data![part])} description={part==='patch'?msg('Compatible fixes'):part==='minor'?msg('Compatible additions'):msg('Breaking changes')}>
    {part==='patch'?msg('Patch {{version}}',{version:options.data[part]}):part==='minor'?msg('Minor {{version}}',{version:options.data[part]}):msg('Major {{version}}',{version:options.data[part]})}
   </Menu.Item>)}
   {options.isPending && <Menu.Item disabled><LoadingState inline>{msg('Loading version options…')}</LoadingState></Menu.Item>}
   {options.isError && <Menu.Item disabled>{msg('Version options unavailable')}</Menu.Item>}
  </Menu.Content></Menu.Portal>
 </Menu.Root></span>
}
