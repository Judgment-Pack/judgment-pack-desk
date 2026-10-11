import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import {PaneWidthControls} from './PaneWidthControls'

afterEach(cleanup)
it('exposes direct width controls, clamps steps and disables controls at bounds', () => {
  const onChange=vi.fn(), onReset=vi.fn()
  const view=(value:number)=><PaneWidthControls value={value} min={600} max={900} onChange={onChange} onReset={onReset}/>
  const rendered=render(view(630))
  expect(screen.queryByRole('menu')).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Narrower'}))
  expect(onChange).toHaveBeenLastCalledWith(600)
  rendered.rerender(view(880))
  fireEvent.click(screen.getByRole('button',{name:'Wider'}))
  expect(onChange).toHaveBeenLastCalledWith(900)
  rendered.rerender(view(900))
  expect(screen.getByRole('button',{name:'Wider'})).toHaveProperty('disabled',true)
  rendered.rerender(view(600))
  expect(screen.getByRole('button',{name:'Narrower'})).toHaveProperty('disabled',true)
  fireEvent.click(screen.getByRole('button',{name:'Reset width'}))
  expect(onReset).toHaveBeenCalledOnce()
})
