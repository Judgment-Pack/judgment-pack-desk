import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { expect, it } from 'vitest'
import { parseRunJSON } from './decode'
import { contextRequest, contextSelector, type ContextRequestKind } from './contextProtocol'
const rows=JSON.parse(readFileSync(resolve(import.meta.dirname,'../../../../internal/desk/testdata/chat-context-protocol.json'),'utf8')) as {name:string;kind:ContextRequestKind|'selector';purpose?:string;raw:string;valid:boolean}[]
for(const row of rows)it(row.name,()=>{
 let accepted=false
 try{const value=parseRunJSON(row.raw);accepted=row.kind==='selector'?contextSelector(value,row.purpose):contextRequest(row.kind,value)}catch{/* malformed JSON is refused before the closed reader */}
 expect(accepted).toBe(row.valid)
})
