import { memberOf, parseJsonText, type JsonNode } from '../research/verify/canon'
/** Compact JSON retains numeric literals, unlike JSON.parse/stringify. */
export function wireJSON(node:JsonNode,ordered=false):string {
 switch(node.kind) {
  case 'object':return '{'+(ordered?node.members:[...node.members].sort((a,b)=>a.name<b.name?-1:a.name>b.name?1:0)).map(m=>JSON.stringify(m.name)+':'+wireJSON(m.value,ordered)).join(',')+'}'
  case 'array':return '['+node.items.map(item=>wireJSON(item,ordered)).join(',')+']'
  case 'number':return node.literal
  case 'null':return 'null'
  default:return JSON.stringify(node.value)
 }
}
export function conversationWire(raw:string,collection='chats'):Record<string,string> {
 const content=memberOf(parseJsonText(raw),'content'),chats=content&&memberOf(content,collection),rows:Record<string,string>={}
 if(chats?.kind!=='array')return rows
 for(const row of chats.items){const id=memberOf(row,'id');if(id?.kind==='string')rows[id.value]=wireJSON(row,true)}
 return rows
}
/** Only fields proven untouched since the retained read reuse their wire literals. */
export function encodeWireChat(chat:{id:string},raw?:string,edited:ReadonlySet<string>=new Set()):string {
 if(!raw)return encodeWireValue(chat)
 const node=parseJsonText(raw),fields=node.kind==='object'?new Map(node.members.map(m=>[m.name,m.value])):new Map<string,JsonNode>()
 return '{'+Object.entries(chat).filter(([,v])=>v!==undefined).map(([key,value])=>{
  const old=fields.get(key)
  const kept=old&&!edited.has(key)?wireJSON(old,true):encodeWireValue(value)
  return JSON.stringify(key)+':'+kept
 }).join(',')+'}'
}
export function wireField(raw:string,key:string):string|undefined {
 const field=memberOf(parseJsonText(raw),key)
 return field&&wireJSON(field,true)
}

// In-memory provenance stays separate from display values and saved JSON.
// Display values remain ordinary JS values; only the writer consumes this tree.
const retainedWire=new WeakMap<object,JsonNode>()
export function retainWireValue<T>(value:T,node:JsonNode):T {
 if(value!==null&&typeof value==='object'){
  retainedWire.set(value,node)
  if(Array.isArray(value)&&node.kind==='array')value.forEach((item,i)=>{if(node.items[i])retainWireValue(item,node.items[i]!)})
  else if(!Array.isArray(value)&&node.kind==='object')for(const member of node.members)if(Object.hasOwn(value,member.name))retainWireValue((value as Record<string,unknown>)[member.name],member.value)
 }
 return value
}
/** Carry provenance only when a host projection deliberately copies a retained value. */
export function copyWireValue<T>(original:unknown,value:T):T {
 const node=original!==null&&typeof original==='object'?retainedWire.get(original):undefined
 if(node&&value!==null&&typeof value==='object')retainedWire.set(value,node)
 return value
}
/** Keep unchanged numeric tokens and member order; explicit edits still win. */
export function encodeWireValue(value:unknown):string {
 const encode=(v:unknown,inherited?:JsonNode):string|undefined=>{
  const node=v!==null&&typeof v==='object'?retainedWire.get(v):inherited
  if(typeof v==='number'&&node?.kind==='number'&&Object.is(v,Number(node.literal)))return node.literal
  if(Array.isArray(v))return '['+v.map((item,i)=>encode(item,node?.kind==='array'?node.items[i]:undefined)??'null').join(',')+']'
  if(v!==null&&typeof v==='object'){
   const fields=new Map(node?.kind==='object'?node.members.map(m=>[m.name,m.value]):[])
   const keys=[...fields.keys()].filter(key=>Object.hasOwn(v,key))
   keys.push(...Object.keys(v).filter(key=>!fields.has(key)))
   return '{'+keys.flatMap(key=>{const text=encode((v as Record<string,unknown>)[key],fields.get(key));return text===undefined?[]:[JSON.stringify(key)+':'+text]}).join(',')+'}'
  }
  return JSON.stringify(v)
 }
 return encode(value)??'null'
}
