import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {afterEach,expect,it} from 'vitest'
import {Tooltip} from 'radix-ui'
import {RecordDetailsTable,type RecordDetailField} from './RecordDetailsTable'
afterEach(cleanup)
const fields:RecordDetailField[]=[{key:'size_sourced',label:'Size sourced',value:'false',state:'AI interpretation',attention:true,changed:true,details:<p>Reviewed pages did not establish a count.</p>},{key:'age',label:'Age',value:'0',state:'Source-backed',details:<blockquote>Published today.</blockquote>},{key:'size',label:'Size',value:'Unknown',state:'Missing',attention:true,details:<p>No retained count.</p>}]
const show=(rows=fields)=><Tooltip.Provider><RecordDetailsTable fields={rows} hasPrevious/></Tooltip.Provider>
it('keeps false, zero and unknown distinct and only expands requested evidence',()=>{
 render(show())
 expect(screen.getByRole('cell',{name:'false'})).toBeTruthy();expect(screen.getByRole('cell',{name:'0'})).toBeTruthy();expect(screen.getByRole('cell',{name:'Unknown'})).toBeTruthy()
 expect(screen.queryByText('Published today.')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Details for Age'}))
 expect(screen.getByText('Published today.')).toBeTruthy();expect(screen.queryByText('No retained count.')).toBeNull()
})
it('filters without losing the original values or expanded detail',()=>{
 render(show());fireEvent.click(screen.getByRole('button',{name:'Details for Age'}))
 fireEvent.change(screen.getByRole('searchbox',{name:'Search fields'}),{target:{value:'size'}})
 expect(screen.queryByText('Published today.')).toBeNull();expect(screen.getByText('2 of 3 fields')).toBeTruthy()
 fireEvent.change(screen.getByRole('searchbox',{name:'Search fields'}),{target:{value:''}})
 expect(screen.getByText('Published today.')).toBeTruthy()
})
it('provides the full long value on demand without dropping its tail',()=>{
 const value='A long supported narrative. '.repeat(15)+'End of the retained value.'
 render(show([{...fields[0],value}]))
 expect(screen.queryByText(value)).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Show more Size sourced'}))
 expect(screen.getByText(value)).toBeTruthy()
})
