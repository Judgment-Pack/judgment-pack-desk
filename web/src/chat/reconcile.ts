import type { Chat } from './store'
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
/** Three-way, field-level reconciliation. Conflicts keep the caller's dirty state. */
export function reconcileChats(base: readonly Chat[], local: readonly Chat[], remote: readonly Chat[], wire: readonly Chat[] = remote): Chat[] {
 const before = new Map(base.map(c=>[c.id,c])), here = new Map(local.map(c=>[c.id,c])), there = new Map(remote.map(c=>[c.id,c])), raw = new Map(wire.map(c=>[c.id,c]))
 const result: Chat[] = []
 for(const id of new Set([...local.map(c=>c.id),...remote.map(c=>c.id)])) {
  const b=before.get(id),l=here.get(id),r=there.get(id)
  if(!b) { if(l&&r&&!same(l,r))throw Error('A new chat conflicts with saved history. Your local changes are kept.');if(l||r)result.push(l??raw.get(id)!);continue }
  if(!l) {if(r&&!same(b,r))throw Error('The deleted chat changed on the server. Your local changes are kept.');continue}
  if(!r) {if(!same(b,l))throw Error('The edited chat was deleted on the server. Your local changes are kept.');continue}
  const row={...raw.get(id)!} as unknown as Record<string,unknown>, old=b as unknown as Record<string,unknown>, current=l as unknown as Record<string,unknown>, saved=r as unknown as Record<string,unknown>
  for(const key of new Set([...Object.keys(old),...Object.keys(current)]))if(!same(old[key],current[key])) {
   if(!same(old[key],saved[key])&&!same(current[key],saved[key]))throw Error('The same chat field changed on the server. Your local changes are kept.')
   if(current[key]===undefined)delete row[key];else row[key]=current[key]
  }
  result.push(row as unknown as Chat)
 }
 return result
}
