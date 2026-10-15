import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { createHash, webcrypto } from 'node:crypto'
import { afterEach, expect, it, vi } from 'vitest'
import { canonicalize, dialectDigest } from './canon'
import { parseMapping, mappedCase } from '../jobs/mappedInputs'
import { setSourceIntegration } from '../jobs/jobIntegrations'
import { jobsAPI } from '../jobs/client'
import { parseMapping as legacyParse, mappedCase as legacyCase } from './__fixtures__/legacy-producer/producer/mappedInputs'
import { setSourceIntegration as legacyIntegration } from './__fixtures__/legacy-producer/producer/jobIntegrations'
import { jobsAPI as legacyAPI } from './__fixtures__/legacy-producer/producer/client'
import { canonicalText } from '../research/verify/canon'
import type { DriveSelection } from '../connections/client'
import type { ProfileEntry } from './__fixtures__/legacy-producer/producer/mappingTypes'

const mocks = vi.hoisted(()=>({fetch:vi.fn()}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:mocks.fetch}))
afterEach(()=>{vi.resetAllMocks();vi.unstubAllGlobals()})
const directory=resolve('src/dialect/__fixtures__/legacy-producer')
type Fixture={producer:string;input:string;integration?:ProfileEntry|null;caseValue:Record<string,unknown>;selections:Record<string,DriveSelection>;request:string;mappingDigest:string}
// Recorded once by running the copied origin/main parser, integration editor,
// case builder and jobsAPI against a captured deskFetch. SHA-256 uses that
// producer's gateway canonical form, as Runner does, not the raw request order.
const fixtures=readdirSync(directory).filter(name=>name.endsWith('.json')).map(name=>({name,...JSON.parse(readFileSync(resolve(directory,name),'utf8')) as Fixture}))

it.each(fixtures)('$name: parse-and-send preserves the frozen digest and changes only mapping member order',async fixture=>{
 vi.stubGlobal('crypto',webcrypto)
 mocks.fetch.mockImplementation(async()=>new Response('{}',{status:200}))
 // Replaying the copied producer checks provenance; it never rewrites the fixture.
 let oldMapping=legacyParse(fixture.input)
 if(fixture.integration!==undefined)oldMapping=legacyIntegration(oldMapping,fixture.integration??undefined)
 await legacyAPI('inputs/next',{source:{mapping:oldMapping,case:legacyCase(oldMapping,fixture.caseValue,fixture.selections),sources:{}}})
 expect(fixture.producer).toBe('8d51f6d4470a885ce904fe0ebbc93053286f9cc9')
 expect(Buffer.from(mocks.fetch.mock.calls[0][1].body)).toEqual(Buffer.from(fixture.request))
 expect('sha256:'+createHash('sha256').update(canonicalText(JSON.stringify(oldMapping))).digest('hex')).toBe(fixture.mappingDigest)

 let mapping=parseMapping(fixture.input)
 if(fixture.integration!==undefined)mapping=setSourceIntegration(mapping,fixture.integration??undefined)
 await jobsAPI('inputs/next',{source:{mapping,case:mappedCase(mapping,fixture.caseValue,fixture.selections),sources:{}}})
 const request=mocks.fetch.mock.calls[1][1].body as string
 const oldRequest=JSON.parse(fixture.request)
 expect(JSON.parse(request)).toEqual(oldRequest)
 const canonical=canonicalize(mapping)
 // Exact UTF-8 comparison: the enclosing request and all other bytes stay put.
 // The only replacement is the old mapping's member order with canon.ts output.
 expect(Buffer.from(request)).toEqual(Buffer.from(fixture.request.replace(JSON.stringify(oldRequest.source.mapping),canonical)))
 expect(Buffer.from(request)).not.toEqual(Buffer.from(fixture.request))
 expect(await dialectDigest(JSON.parse(request).source.mapping)).toBe(fixture.mappingDigest)
 expect(mocks.fetch).toHaveBeenLastCalledWith('/api/operations/inputs/next',expect.objectContaining({method:'POST',body:request}))
})
