import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ChatComposer } from './ChatComposer'
import { Button } from '../ui/Button'
afterEach(cleanup)
const props = {id:'compose',value:'A request',onChange:vi.fn(),onSend:vi.fn(),onStop:vi.fn(),running:false,placeholder:'Message',controls:<Button>Add source</Button>}
it('uses Enter and the send button once while preserving modified Enter and IME input', () => {
  const send=vi.fn();render(<ChatComposer {...props} onSend={send}/> )
  const input=screen.getByRole('textbox',{name:'Message the assistant'})
  fireEvent.keyDown(input,{key:'Enter',shiftKey:true});fireEvent.keyDown(input,{key:'Enter',ctrlKey:true})
  fireEvent.keyDown(input,{key:'Enter',isComposing:true});fireEvent.keyDown(input,{key:'Enter',keyCode:229})
  expect(send).not.toHaveBeenCalled()
  fireEvent.keyDown(input,{key:'Enter'});expect(send).toHaveBeenCalledTimes(1)
  fireEvent.keyDown(input,{key:'Enter',repeat:true});expect(send).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button',{name:'Send'}));expect(send).toHaveBeenCalledTimes(2)
  fireEvent.click(screen.getByRole('button',{name:'Add source'}));expect(send).toHaveBeenCalledTimes(2)
})
it('respects submission guards and keeps stop available during a run', () => {
  const send=vi.fn(),stop=vi.fn();const view=render(<ChatComposer {...props} sendDisabled onSend={send} onStop={stop}/>)
  fireEvent.keyDown(screen.getByRole('textbox'),{key:'Enter'});fireEvent.click(screen.getByRole('button',{name:'Send'}));expect(send).not.toHaveBeenCalled()
  view.rerender(<ChatComposer {...props} running disabled onSend={send} onStop={stop}/>)
  fireEvent.click(screen.getByRole('button',{name:'Stop'}));expect(stop).toHaveBeenCalledOnce()
  expect(screen.getByText('Working in this window. You can switch chats; keep this window open.')).toBeTruthy()
})
