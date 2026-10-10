import {useState} from 'react'
import {cleanup, fireEvent, render, screen, within} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import {MemoryRouter} from 'react-router-dom'
import {GraphComposition} from './GraphComposition'
const seed = {formatVersion:'1',id:'flow',version:'0.1.0',nodes:{first:{pack:'a'},last:{pack:'b'}},edges:[{from:'first',to:'last',fact:'/eligibility'}],result:'last'}
afterEach(()=>{cleanup();localStorage.clear()})
function Fixture({change = () => {}}:{change?:(s:string)=>void}) {
 const [content,setContent]=useState(JSON.stringify(seed))
 return <MemoryRouter><GraphComposition content={content} packs={['a','b','c']} onChange={next=>{change(next);setContent(next)}}/></MemoryRouter>
}
it('keeps the selected editor mounted across collapse and expansion, without changing graph bytes',()=>{
 const change=vi.fn();render(<Fixture change={change}/>);fireEvent.click(screen.getByRole('radio',{name:'List'}))
 fireEvent.click(screen.getByRole('button',{name:'firsta'}))
 const description=screen.getByLabelText('Description')
 fireEvent.change(description,{target:{value:'Shared decision'}})
 expect(JSON.parse(change.mock.lastCall![0]).nodes.first.description).toBe('Shared decision')
 fireEvent.click(screen.getByRole('button',{name:'Expand editor'}));expect(screen.getByLabelText('Description')).toBe(description)
 fireEvent.click(screen.getByRole('button',{name:'Restore editor'}));expect(screen.getByLabelText('Description')).toBe(description)
 fireEvent.click(screen.getByRole('button',{name:'Close editor'}));fireEvent.click(screen.getByRole('button',{name:'Editor'}))
 expect(screen.getByLabelText('Description')).toBe(description);expect(change).toHaveBeenCalledTimes(1)
})
it('edits a mapping below the canvas and never changes it into a control-flow edge',()=>{
 const change=vi.fn();render(<Fixture change={change}/>);fireEvent.click(screen.getByRole('radio',{name:'List'}))
 fireEvent.click(screen.getByRole('button',{name:/first → last/}));fireEvent.change(screen.getByLabelText('Fact destination'),{target:{value:'/review/eligibility'}})
 expect(JSON.parse(change.mock.lastCall![0]).edges).toEqual([{from:'first',to:'last',fact:'/review/eligibility'}])
})
it('adds a reusable pack and selects its bottom editor',()=>{
 const change=vi.fn();render(<Fixture change={change}/>);fireEvent.click(screen.getByRole('button',{name:'Add pack'}))
 fireEvent.change(screen.getByLabelText('Search packs'),{target:{value:'c'}})
 fireEvent.click(within(screen.getByRole('dialog')).getByRole('button',{name:'c'}))
 expect(JSON.parse(change.mock.lastCall![0]).nodes.c).toEqual({pack:'c'})
 expect(screen.getByRole('region',{name:'c'})).toBeTruthy()
})
