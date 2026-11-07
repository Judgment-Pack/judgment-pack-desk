import {describe,it,expect} from 'vitest'
import vectors from '../../../../internal/desk/testdata/dialect_read_recovery_public.json'
import {decodeDialectReadRecovery,recoveryDecimal,MAX_DIALECT_RECOVERY_BYTES,MAX_RUN_BYTES} from './decode'

const fixture=vectors as {version:number;valid:unknown;mutations:readonly {name:string;path:readonly string[];remove?:boolean;value?:unknown}[]}
const run='arun_ffffffffffffffffffffffffffffffff'
describe('shared retained read display contract',()=>{
 it('retains all typed states and uint64 decimal identities without Number conversion',()=>{
  const value=decodeDialectReadRecovery(fixture.valid,run)
  expect(value.runRevision).toBe('9007199254740993')
  expect(value.recoveryRevision).toBe('18446744073709551615')
  expect(value.reads).toHaveLength(11)
  expect(value.reads[1]).toMatchObject({originalOutcome:'interrupted',backingState:'present-valid',storageState:'accounted-retained'})
  expect(value.reads.at(-1)?.ordinal).toBe('18446744073709551615')
  expect(MAX_RUN_BYTES).toBe(16*1024*1024)
  expect(MAX_DIALECT_RECOVERY_BYTES).toBe(1024*1024)
 })
 for(const mutation of fixture.mutations)it(`refuses ${mutation.name}`,()=>{
  const value=structuredClone(fixture.valid) as Record<string,unknown>
  let target:unknown=value
  for(const key of mutation.path.slice(0,-1))target=(target as Record<string,unknown>)[key]
  const object=target as Record<string,unknown>,key=mutation.path.at(-1)!
  if(mutation.remove)delete object[key];else object[key]=mutation.value
  expect(()=>decodeDialectReadRecovery(value,run)).toThrow()
 })
 it('checks string boundaries without accepting rounded/numeric CAS identities',()=>{
  for(const value of ['9007199254740993','18446744073709551615'])expect(recoveryDecimal(value,false)).toBe(true)
  for(const value of [9007199254740992,'18446744073709551616','01','1e3','-1','','1.0'])expect(recoveryDecimal(value)).toBe(false)
 })
})
