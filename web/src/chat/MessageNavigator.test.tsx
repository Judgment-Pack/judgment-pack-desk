import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createRef, useState } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { MessageNavigator, messageEntries } from './MessageNavigator'
import { CopyMessage } from './MessageRenderer'
import type { Turn } from '../research/run'
afterEach(()=>{cleanup();vi.restoreAllMocks()})
const turns:Turn[]=[{id:'one',role:'user',kind:'brief',text:'First question',at:'2026-10-11T12:00:00Z'},{id:'reply',role:'assistant',kind:'message',text:'An answer',at:'2026-10-11T12:01:00Z'},{id:'two',role:'user',kind:'message',text:'Another question',at:'2026-10-10T12:01:00Z'},{id:'file',role:'user',kind:'message',text:'',attachments:[{id:'file',name:'brief.txt',text:'private file content'}],at:''}]
it('keeps user messages in submitted order, with attachment labels and stable ids',()=>{
 expect(messageEntries(turns).map(row=>[row.id,row.text])).toEqual([['one','First question'],['two','Another question'],['file','brief.txt']])
})
it('opens at the bottom each time, filters without renumbering, and jumps only after closing',async()=>{
 const jump=vi.fn()
 const getHeight=vi.spyOn(HTMLElement.prototype,'scrollHeight','get').mockReturnValue(900)
 function Harness(){const [open,setOpen]=useState(false);return <MessageNavigator turns={turns} open={open} onOpenChange={setOpen} onJump={jump} threadRef={createRef()}/>}
 render(<Harness/>);const trigger=screen.getByRole('button',{name:'Jump to message'});fireEvent.click(trigger)
 const input=await screen.findByRole('searchbox',{name:'Find a message…'}), list=screen.getByRole('list',{name:'Your messages'})
 expect(list.scrollTop).toBe(900);expect(document.activeElement).toBe(input)
 fireEvent.change(input,{target:{value:'Another'}})
 expect(list.scrollTop).toBe(0);expect(screen.getByRole('button',{name:/02 Another question/})).toBeTruthy()
 expect(screen.queryByRole('button',{name:/First question/})).toBeNull()
 fireEvent.keyDown(input,{key:'Escape'});await waitFor(()=>expect(document.activeElement).toBe(trigger));expect(jump).not.toHaveBeenCalled()
 fireEvent.click(trigger);await screen.findByRole('searchbox',{name:'Find a message…'})
 expect(screen.getByRole('list',{name:'Your messages'}).scrollTop).toBe(900)
 fireEvent.click(screen.getByRole('button',{name:/01 First question/}))
 await waitFor(()=>expect(jump).toHaveBeenCalledExactlyOnceWith('one'))
 expect(screen.queryByRole('textbox',{name:'Find a message…'})).toBeNull();getHeight.mockRestore()
})
it('copies the exact user text and keeps additional actions accessible',async()=>{
 const writeText=vi.fn(async()=>{});Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText}})
 render(<CopyMessage text={'Line 1\n\njudgmentpack.org'} label="Copy message"><button>Retry response</button></CopyMessage>)
 fireEvent.click(screen.getByRole('button',{name:'Copy message'}))
 await waitFor(()=>expect(writeText).toHaveBeenCalledExactlyOnceWith('Line 1\n\njudgmentpack.org'))
 expect(screen.getByRole('button',{name:'Retry response'})).toBeTruthy()
})
