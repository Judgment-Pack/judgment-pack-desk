import {cleanup,render,screen} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {Tooltip} from 'radix-ui'
import {MemoryRouter} from 'react-router-dom'
import {TestsCollection} from './TestsCollection'
import {emptySuite,importMatrix} from './model'
afterEach(cleanup)
const props={suite:emptySuite(),digest:'v1',query:'',onQuery:vi.fn(),onReset:vi.fn(),filter:'all',onFilter:vi.fn(),sort:'saved',onSort:vi.fn(),busy:false,canRun:true,header:null,caseHref:()=>'/case',onRun:vi.fn(),onResult:vi.fn(),onDuplicate:vi.fn(),onDelete:vi.fn(),onExport:vi.fn(),coverage:0,onCoverage:vi.fn(),onDesign:vi.fn(),loading:false}
const wrap=(extra:Partial<typeof props>&{unavailable?:boolean})=><Tooltip.Provider><MemoryRouter><TestsCollection {...props} {...extra}/></MemoryRouter></Tooltip.Provider>
it('does not report an empty suite or item count when the read failed',()=>{
 render(wrap({unavailable:true}))
 expect(screen.getByText('Tests unavailable')).toBeTruthy()
 expect(screen.queryByText('No saved cases yet')).toBeNull()
 expect(screen.queryByText('0 items')).toBeNull()
})
it('shows cached cases during a failed refresh and an empty state only after a successful read',()=>{
 const suite={...emptySuite(),cases:importMatrix({cases:[{id:'saved-case',facts:{},expectedDisposition:{kind:'outcome',outcomeId:'accept',reasons:[],handoff:{state:'none'}}}]},'manual')}
 const view=render(wrap({suite}))
 expect(screen.getByRole('link',{name:'saved-case'})).toBeTruthy()
 view.rerender(wrap({suite:emptySuite()}))
 expect(screen.getByText('No saved cases yet')).toBeTruthy()
})
