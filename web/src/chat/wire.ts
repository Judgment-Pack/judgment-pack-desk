import { memberOf, parseJsonText, type JsonNode } from '../research/verify/canon'
/** Compact JSON retains numeric literals, unlike JSON.parse/stringify. */
export function wireJSON(node:JsonNode):string {
 switch(node.kind) {
  case 'object':return '{'+[...node.members].sort((a,b)=>a.name<b.name?-1:a.name>b.name?1:0).map(m=>JSON.stringify(m.name)+':'+wireJSON(m.value)).join(',')+'}'
  case 'array':return '['+node.items.map(wireJSON).join(',')+']'
  case 'number':return node.literal
  case 'null':return 'null'
  default:return JSON.stringify(node.value)
 }
}
export function conversationWire(raw:string):Record<string,string> {
 const content=memberOf(parseJsonText(raw),'content'),chats=content&&memberOf(content,'chats'),rows:Record<string,string>={}
 if(chats?.kind!=='array')return rows
 for(const row of chats.items){const id=memberOf(row,'id');if(id?.kind==='string')rows[id.value]=wireJSON(row)}
 return rows
}
/** Only fields proven untouched since the retained read reuse their wire literals. */
export function encodeWireChat(chat:{id:string},raw?:string,edited:ReadonlySet<string>=new Set()):string {
 if(!raw)return JSON.stringify(chat)
 const node=parseJsonText(raw),fields=node.kind==='object'?new Map(node.members.map(m=>[m.name,m.value])):new Map<string,JsonNode>()
 return '{'+Object.entries(chat).filter(([,v])=>v!==undefined).map(([key,value])=>{
  const old=fields.get(key)
  const kept=old&&!edited.has(key)?wireJSON(old):JSON.stringify(value)
  return JSON.stringify(key)+':'+kept
 }).join(',')+'}'
}
export function wireField(raw:string,key:string):string|undefined {
 const field=memberOf(parseJsonText(raw),key)
 return field&&wireJSON(field)
}
