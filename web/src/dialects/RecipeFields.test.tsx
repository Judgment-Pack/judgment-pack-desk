import {useState} from 'react'
import {cleanup,fireEvent,render,screen,within} from '@testing-library/react'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {RecipeFields} from './RecipeFields'
import type {ResearchRecipe} from './research'

afterEach(cleanup)
const fields:ResearchRecipe['fields']=Array.from({length:30},(_,index)=>({name:'field_'+index,description:'Complete research instructions '+index+' — '+('Long detail. '.repeat(20)),type:index%2?'number':'string'}))
it('edits every field directly and filters without committing, dropping values or changing unrelated types',()=>{
 const changed=vi.fn()
 function Editor(){const [value,setValue]=useState(fields);return <RecipeFields value={value} disabled={false} onChange={next=>{changed(next);setValue(next)}}/>}
 render(<Tooltip.Provider><Editor/></Tooltip.Provider>);const table=screen.getByRole('table',{name:'Requested fields'})
 expect(within(table).getAllByRole('row')).toHaveLength(31)
 expect(screen.getAllByRole('textbox',{name:'Description'})).toHaveLength(30)
 expect(screen.queryByRole('button',{name:'Done'})).toBeNull()
 expect(changed).not.toHaveBeenCalled()
 const row=within(table).getAllByRole('row')[2]
 expect((within(row).getByRole('textbox',{name:'Description'}) as HTMLTextAreaElement).value).toBe(fields[1].description)
 fireEvent.change(within(row).getByRole('textbox',{name:'Description'}),{target:{value:'Reviewed instruction'}})
 expect(changed.mock.calls.at(-1)![0]).toEqual(fields.map((field,index)=>index===1?{...field,description:'Reviewed instruction'}:field))
 fireEvent.change(screen.getByRole('searchbox',{name:'Search fields'}),{target:{value:'field_29'}})
 expect(within(table).getAllByRole('row')).toHaveLength(2)
 expect((screen.getByRole('textbox',{name:'Field name'}) as HTMLInputElement).value).toBe('field_29')
 fireEvent.change(screen.getByRole('searchbox',{name:'Search fields'}),{target:{value:''}})
 expect((screen.getAllByRole('textbox',{name:'Description'})[1] as HTMLTextAreaElement).value).toBe('Reviewed instruction')
})
it('adds and removes fields in the parent buffer and prevents removing the last field',()=>{
 function Editor(){const [value,setValue]=useState(fields.slice(0,1));return <RecipeFields value={value} disabled={false} onChange={setValue}/>}
 render(<Tooltip.Provider><Editor/></Tooltip.Provider>)
 expect((screen.getByRole('button',{name:'Remove field field_0'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Add field'}))
 const name=screen.getAllByRole('textbox',{name:'Field name'})[1] as HTMLInputElement
 expect(name.value).toBe('')
 fireEvent.change(name,{target:{value:'new_field'}})
 fireEvent.click(screen.getByRole('button',{name:'Remove field new_field'}))
 expect(screen.getAllByRole('textbox',{name:'Field name'})).toHaveLength(1)
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'Add field'}))
})

it('shows portable types and keeps legacy fields unchanged until edited',()=>{
 const changed=vi.fn()
 const value:ResearchRecipe['fields']=[{name:'old',description:'Saved text',type:'string'},...(['integer','decimal','boolean','text-list','range'] as const).map(type=>({name:type.replace('-','_'),description:'Portable '+type,type}))]
 render(<Tooltip.Provider><RecipeFields value={value} disabled={false} onChange={changed}/></Tooltip.Provider>)
 const table=screen.getByRole('table',{name:'Requested fields'})
 expect(within(table).getAllByRole('combobox',{name:'Type'}).map(element=>element.textContent)).toEqual(expect.arrayContaining([expect.stringContaining('Text'),expect.stringContaining('Integer'),expect.stringContaining('Decimal'),expect.stringContaining('Text list'),expect.stringContaining('Range')]))
 expect(changed).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Add field'}))
 expect(changed.mock.calls.at(-1)![0].at(-1)).toMatchObject({type:'text'})
 expect(changed.mock.calls.at(-1)![0][0]).toEqual(value[0])
})
