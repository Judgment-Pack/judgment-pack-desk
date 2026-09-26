import { expect,it } from 'vitest'
import { compareRuns } from './comparison'
import { importMatrix, type TestRun } from './model'
const cases=importMatrix({matrixVersion:'3',cases:[{id:'one',facts:{value:0},expectedErrorClass:'policy'}]},'manual')
const run={id:'r',cases,report:{packs:[{rows:[{id:'one',status:'passed',actual:'answer',expected:'answer'}]}]}} as TestRun
it('separates changed expectations from changed inputs without inferring disposition equality from display strings',()=>{
 const after=structuredClone(run)
 after.cases[0]!.row.expectedErrorClass='other'
 after.report!.packs![0]!.rows![0]!.status='mismatch'
 const result=compareRuns(run,after)[0]!
 expect(result.inputsChanged).toBe(false);expect(result.expectationChanged).toBe(true)
 expect(result.before?.status).toBe('passed');expect(result.after?.status).toBe('mismatch')
 after.cases[0]!.row.facts={value:false};expect(compareRuns(run,after)[0]!.inputsChanged).toBe(true)
 expect(run.cases[0]!.row.facts).toEqual({value:0})
})
it('shows removed/added cases and unavailable results instead of inventing regressions',()=>{
 const after={...run,cases:[{...cases[0]!,id:'two',row:{...cases[0]!.row,id:'two'}}],report:undefined,error:'offline'}
 const rows=compareRuns(run,after)
 expect(rows.map(r=>r.presence)).toEqual(['removed','added'])
 expect(rows[1]!.before).toBeUndefined();expect(rows[1]!.after).toBeUndefined()
})
