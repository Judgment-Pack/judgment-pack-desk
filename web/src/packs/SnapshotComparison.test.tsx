import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect,it } from 'vitest'
import { SnapshotComparison } from './SnapshotComparison'
afterEach(cleanup)
it('compares rules by identity and keeps sources and outcomes in the review',()=>{
 render(<SnapshotComparison before={JSON.stringify({rules:[{id:'a',value:1}],sources:[],outcomes:[{id:'accept'}]})} after={JSON.stringify({rules:[{id:'a',value:2}],sources:[{id:'policy'}],outcomes:[{id:'review'}]})}/>)
 expect(screen.getByText('3 pack sections changed')).toBeTruthy()
 expect(screen.getByText('rules · Changed')).toBeTruthy()
 expect(screen.getByText('sources · Changed')).toBeTruthy()
 expect(screen.getByText('outcomes · Changed')).toBeTruthy()
})
it('does not fabricate a comparison when snapshot bytes are invalid',()=>{
 render(<SnapshotComparison before='invalid' after='{}'/>);expect(screen.getByText('These snapshots could not be compared.')).toBeTruthy()
})
