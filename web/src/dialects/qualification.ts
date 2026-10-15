import type { PackDocument } from '../mcp/types'
import { pointerEscape } from '../jobs/sourceInputs'
import { sha256Hex } from '../research/verify/receipt'
import { msg } from '../i18n'
import type { DialectPackBinding, DialectRecord } from './client'
import type { ResearchRecipe } from './research'

export function recordColumns(records: DialectRecord[], research?: ResearchRecipe) {
 // Planned fields are choices only; missing values stay absent in conversions.
 const planned=research?['name','url','website',...research.fields.map(f=>f.name),...((research.target==='people-at-companies'||research.companyInputs?.length||research.companyIds?.length)?['company_id','company_name','company_url']:[])]:[]
 return [...new Set([...records.flatMap(row=>Object.keys(row.values)),...planned])].sort().map(name=>({value:'/values/'+pointerEscape(name),label:name}))
}
export async function packBinding(key:string,pack:string):Promise<DialectPackBinding> {
 const document=JSON.parse(pack) as PackDocument
 if(!document||typeof document.id!=='string'||typeof document.version!=='string'||document.title!==undefined&&typeof document.title!=='string'||!document.decision||typeof document.decision!=='object'||document.evidenceRequirements!==undefined&&(!Array.isArray(document.evidenceRequirements)||document.evidenceRequirements.some(e=>!e||typeof e.id!=='string'||e.description!==undefined&&typeof e.description!=='string')))throw Error(msg('The pack could not be loaded.'))
 return {kind:'pack',key,pack,digest:'sha256:'+await sha256Hex(new TextEncoder().encode(pack)),mapping:{version:2,case:{facts:[],evidence:[]},sources:[]}}
}
export function boundPack(binding:DialectPackBinding):PackDocument { return JSON.parse(binding.pack) as PackDocument }
