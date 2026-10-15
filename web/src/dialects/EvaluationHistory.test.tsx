import {cleanup,render,screen} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,expect,it} from 'vitest'
import {EvaluationResult,latestRecordEvaluation} from './EvaluationHistory'
import type {DialectEvaluation} from './client'
afterEach(cleanup)
it('keeps completed decisions separate from signing failures and earlier recipes',()=>{
 const batches=[{id:'eval',jobId:'job',recipeVersion:1,rows:[{recordId:'row',runId:'run',state:'completed',signing:{status:'failed',reason:'key unavailable'}}]}] as DialectEvaluation[]
 render(<MemoryRouter><EvaluationResult result={latestRecordEvaluation(batches,'row')} recipeVersion={2}/></MemoryRouter>)
 expect(screen.getByRole('link').getAttribute('href')).toBe('/jobs/job/runs/run')
 expect(screen.getByText('Earlier recipe')).toBeTruthy();expect(screen.getByText('Signing failed')).toBeTruthy()
 expect(screen.getByRole('link').textContent).toContain('Completed')
})
it('does not invent an outcome for a record with no run',()=>{render(<EvaluationResult recipeVersion={1}/>);expect(screen.getByText('Not run')).toBeTruthy()})
