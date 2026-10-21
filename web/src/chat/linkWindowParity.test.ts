import {describe,it,expect} from 'vitest'
import vectors from '../../../internal/desk/testdata/assistant-link-window.json'
import fixture from '../documents/__fixtures__/web-snapshot.json'
import type {DocumentRecord} from '../documents/record'
import type {VerifiedDocument} from '../documents/client'
import {CONTEXT_DEFAULTS} from '@judgment-pack/harness/context/policy'
import {linkReading} from './linkTools'

describe('shared private/browser link window vectors',()=>{
 for(const vector of vectors)it(vector.name,async()=>{
  const record=structuredClone(fixture) as unknown as DocumentRecord
  const basePages=vector.pageCount?Array.from({length:vector.pageCount},()=>vector.pages[0]!):vector.pages
  const pages=basePages.map((page,index)=>index===0?(vector.repeat? page.repeat(vector.repeat):page)+'x'.repeat(vector.padding??0)+(vector.suffix??''):page)
  record.content.pages=pages.map((text,index)=>({...record.content.pages[0]!,number:index+1,text,chars:[...text].length}))
  record.content.pageCount=pages.length;record.content.chars=pages.reduce((sum,text)=>sum+[...text].length,0)
  const url='https://example.org/report'
  record.provenance.source.requestedUrl=url;record.provenance.source.url=url
  const reference={id:'11111111-1111-1111-1111-111111111111',digest:`sha256:${'a'.repeat(64)}`,pages:pages.map((_,index)=>index+1),allowPartial:false}
  const document={record,digest:reference.digest} as VerifiedDocument
  const file={id:reference.id,name:record.document.name,text:'',document:reference,link:{url}}
  let acquisitions=0
  const tool=linkReading({available:()=>true,researchEnabled:()=>false,config:()=>({gateway:{url:'http://127.0.0.1:8787',authority:'gateway:test',signer:{algorithm:'ed25519' as const,public:'ab'.repeat(32)}},sources:{search:null,read:null,web:null},limits:{searches:8,reads:12,bytes:8388608,seconds:600},documents:{enabled:true,source:'documents',maxFileBytes:16777216,maxRequestBytes:33554432,maxResponseBytes:16777216}}),documents:()=>[file],addDocument:()=>{throw Error('unexpected save')},log:()=>{},load:async()=>document,ingest:async()=>{acquisitions++;throw Error('unexpected acquisition')}})({turns:()=>[{id:'user',role:'user',kind:'message',text:url,at:'2026-10-21T00:00:00Z'}],policy:()=>CONTEXT_DEFAULTS}).find(tool=>tool.name==='read_link')!
  const result=await tool.execute({url,...(vector.offset===undefined?{}:{offset:vector.offset}),...(vector.find===undefined?{}:{find:vector.find})},new AbortController().signal)
  if(vector.expectedRefusal){expect(result.isError).toBe(true);expect(new TextEncoder().encode(JSON.stringify(result)).length).toBeLessThanOrEqual(CONTEXT_DEFAULTS.toolResultBytes);return}
  const detail=result.structuredContent as {offset:number;pages:number[];matches:{start:number;end:number}[];more:boolean}
  expect(result.isError).not.toBe(true);expect(detail.offset).toBe(vector.expectedOffset);expect(detail.pages).toEqual(vector.expectedPages)
  if(vector.expectedMatches)expect(detail.matches).toEqual(vector.expectedMatches)
  const text=result.content?.[0]?.text??''
  expect(text).not.toMatch(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u)
  expect(new TextEncoder().encode(JSON.stringify(result)).length).toBeLessThanOrEqual(CONTEXT_DEFAULTS.toolResultBytes)
  if(vector.continuation){const match=/showing characters (\d+)–(\d+) of (\d+)/.exec(text)!;const end=Number(match[2]);expect(detail.more).toBe(true);expect(end).toBeGreaterThan(detail.offset);expect(end%2).toBe(0);const next=await tool.execute({url,offset:end},new AbortController().signal);expect((next.structuredContent as {offset:number}).offset).toBe(end)}
  expect(acquisitions).toBe(0)
 })
})
