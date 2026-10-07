import { CODEX_EFFORTS, ASSISTANT_THINKING, type AssistantConfig, type AssistantAgentConfig, type ThinkingTier } from './deskConfig'
import { sourceMessage } from '../i18n/source'

export const ASSISTANT_PROFILE_PATH = 'jpack-assistant.json'
export const PROFILE_PROBLEM = sourceMessage('Desk model preferences could not be read. Fix jpack-assistant.json or reload it in Admin › Assistant.')
export type ModelPreferences = { inherit: true } | {
  inherit: false; models: string[]; model: string | null
  effort?: AssistantAgentConfig['effort']; thinking?: ThinkingTier
}
export interface AssistantProfile { profileVersion: 1 | 2; codex?: ModelPreferences; api?: ModelPreferences; inherit?:boolean; connections?:string[]; defaultConnection?:string|null; models?:Record<string,ModelPreferences> }
export interface AssistantProfileRead {
  present: boolean; sha256?: string; text?: string; value?: AssistantProfile; problem?: string
}
export const INHERITED_PROFILE: AssistantProfile = { profileVersion: 1, codex: { inherit: true }, api: { inherit: true } }
const object = (value: unknown): value is Record<string,unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
export function decodeAssistantProfile(text: string): AssistantProfile {
  const value:unknown=JSON.parse(text)
  if(object(value)&&value.profileVersion===2)return decodeProfileV2(value,text)
  if(new TextEncoder().encode(text).byteLength>65536 || !object(value) || value.profileVersion!==1 || Object.keys(value).some(key=>!['profileVersion','codex','api'].includes(key))) throw new Error(PROFILE_PROBLEM)
  for(const key of ['codex','api'] as const){
    const row=value[key];if(row===undefined)continue
    if(!object(row)||typeof row.inherit!=='boolean')throw new Error(PROFILE_PROBLEM)
    const keys=row.inherit?['inherit']:['inherit','models','model',key==='codex'?'effort':'thinking']
    if(Object.keys(row).some(key=>!keys.includes(key)))throw new Error(PROFILE_PROBLEM)
    if(row.inherit)continue
    if(!Array.isArray(row.models)||row.models.length>128||row.models.some(id=>typeof id!=='string'||!id||id.trim()!==id||new TextEncoder().encode(id).byteLength>128||/[\r\n\0]/.test(id))||new Set(row.models).size!==row.models.length)throw new Error(PROFILE_PROBLEM)
    if(row.models.length===0?row.model!==null:typeof row.model!=='string'||!row.models.includes(row.model))throw new Error(PROFILE_PROBLEM)
    if(row.effort!==undefined&&!(CODEX_EFFORTS as readonly unknown[]).includes(row.effort))throw new Error(PROFILE_PROBLEM)
    if(row.thinking!==undefined&&!(ASSISTANT_THINKING as readonly unknown[]).includes(row.thinking))throw new Error(PROFILE_PROBLEM)
  }
  return value as unknown as AssistantProfile
}
/** Only model preferences are overlaid. Endpoints, accounts and tool grants stay machine-owned. */
export function applyAssistantProfile(machine: AssistantConfig, read?: AssistantProfileRead): AssistantConfig {
  const profile=read?.value
  const agent=profile?.codex,api=profile?.api
  return {...machine,
    ...(machine.agent&&agent&&!agent.inherit?{agent:{...machine.agent,models:[...agent.models],model:agent.model,effort:agent.effort}}:{}),
    ...(machine.endpoint&&api&&!api.inherit?{endpoint:{...machine.endpoint,models:[...api.models],model:api.model},thinking:api.thinking??machine.thinking}:{})}
}

function decodeProfileV2(value:Record<string,unknown>,text:string):AssistantProfile {
 const fail=()=>{throw new Error(PROFILE_PROBLEM)}
 if(new TextEncoder().encode(text).byteLength>65536||typeof value.inherit!=='boolean')return fail()
 const keys=value.inherit?['profileVersion','inherit']:['profileVersion','inherit','connections','defaultConnection','models']
 if(Object.keys(value).some(k=>!keys.includes(k)))return fail()
 if(!value.inherit){
  if(!Array.isArray(value.connections)||value.connections.length>32||value.connections.some(id=>typeof id!=='string'||!/^(legacy-api|legacy-codex|ai-[a-f0-9]{24})$/.test(id))||new Set(value.connections).size!==value.connections.length)return fail()
  if(value.connections.length===0?value.defaultConnection!==null:!value.connections.includes(value.defaultConnection))return fail()
  if(value.models!==undefined){if(!object(value.models)||Object.keys(value.models).length>32)return fail();for(const [id,row] of Object.entries(value.models)){if(!value.connections.includes(id)||!object(row))return fail();decodeAssistantProfile(JSON.stringify({profileVersion:1,[row.effort!==undefined?'codex':'api']:row}))}}
 }
 return value as unknown as AssistantProfile
}
