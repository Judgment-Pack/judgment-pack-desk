import { expect, it } from 'vitest'
import { relationshipPaths } from './relationshipPaths'
const connections = [['root','a'],['a','x'],['a','y'],['b','x'],['b','z'],['exception','a']].map(([source,target])=>({id:`${source}-${target}`,source:source!,target:target!}))
const nodes = (selected: string) => ['root','a','b','x','y','z','exception','alone'].map(id=>({id,selected:id===selected}))
it('highlights every branch through a selected rule without flooding sibling contributors',()=>{
 const path=relationshipPaths(nodes('a'),connections)
 expect([...path.nodeIds].sort()).toEqual(['a','exception','root','x','y'])
 expect([...path.edgeIds].sort()).toEqual(['a-x','a-y','exception-a','root-a'])
})
it('highlights all upstream merges for an outcome without following sibling outcomes',()=>{
 const path=relationshipPaths(nodes('x'),connections)
 expect([...path.nodeIds].sort()).toEqual(['a','b','exception','root','x'])
 expect([...path.edgeIds].sort()).toEqual(['a-x','b-x','exception-a','root-a'])
})
it('handles an isolated selection, clearing selection and dangling edges',()=>{
 expect([...relationshipPaths(nodes('alone'),connections).nodeIds]).toEqual(['alone'])
 expect(relationshipPaths(nodes(''),connections).active).toBe(false)
 expect(relationshipPaths(nodes('a'),[{id:'bad',source:'a',target:'missing'}]).edgeIds.size).toBe(0)
})
it('terminates on cycles and long paths without recursion',()=>{
 const all=Array.from({length:10000},(_,i)=>({id:String(i),selected:i===5000}))
 const edges=all.slice(1).map((node,i)=>({id:node.id,source:String(i),target:node.id}))
 edges.push({id:'cycle',source:'9999',target:'0'})
 const result=relationshipPaths(all,edges)
 expect(result.nodeIds.size).toBe(10000);expect(result.edgeIds.size).toBe(10000)
})
