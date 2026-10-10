import { beforeEach, expect, it, vi } from 'vitest'
import { readFile } from '../files/client'
import { compareGraphRevision, hasGraphProvenance } from './provenance'
vi.mock('../files/client', async original => ({...await original(), readFile:vi.fn()}))
const a='a'.repeat(64),b='b'.repeat(64),c='c'.repeat(64)
const value={graphSha256:a,configSha256:b,nodes:[{pack:'p',packSha256:c},{pack:'p',packSha256:c}]}
beforeEach(()=>{vi.resetAllMocks();vi.mocked(readFile).mockImplementation(async path=>({path,content:path==='jpack.json'?'{"packs":{"p":{"path":"p.json"}},"graphs":{"g":{"path":"g.json"}}}':'{}',bytes:2,sha256:path==='jpack.json'?b:path==='g.json'?a:c}))})
it('compares exact bytes for graph, configuration and repeated pack nodes',async()=>{
 expect(await compareGraphRevision('g',value,new AbortController().signal)).toBe('matching')
 expect(readFile).toHaveBeenCalledTimes(3)
 expect(await compareGraphRevision('g',{...value,graphSha256:c},new AbortController().signal)).toBe('changed')
 expect(await compareGraphRevision('g',{...value,nodes:[{pack:'p',packSha256:a}]},new AbortController().signal)).toBe('changed')
})
it('never treats an old or partial payload as a matching result',async()=>{
 for(const result of [{},{graphSha256:a}, {...value,nodes:[{pack:'p'}]}]) {
  expect(hasGraphProvenance(result)).toBe(false)
  expect(await compareGraphRevision('g',result,new AbortController().signal)).toBe('unavailable')
 }
 expect(readFile).not.toHaveBeenCalled()
})

it('does not use a runtime bundle digest as a graph document binding',()=>{
  expect(hasGraphProvenance({configSha256:'a'.repeat(64), nodes:[{pack:'p',packSha256:'b'.repeat(64)}],artifact:{bundleDigest:'c'.repeat(64)}} as Parameters<typeof hasGraphProvenance>[0])).toBe(false)
})
