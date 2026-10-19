import { createRef } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { SearchInput } from './SearchInput'

afterEach(cleanup)
it('keeps label, ref, input events and a trailing scope control on separate native controls', () => {
  const ref = createRef<HTMLInputElement>(), changed = vi.fn(), key = vi.fn(), scope = vi.fn()
  render(<><label htmlFor="find">Search records</label><SearchInput id="find" ref={ref} onChange={changed} onKeyDown={key} suffix={<button type="button" onClick={scope}>This folder</button>}/></>)
  const input = screen.getByRole('searchbox', { name: 'Search records' })
  expect(ref.current).toBe(input)
  ref.current?.focus()
  expect(document.activeElement).toBe(input)
  fireEvent.change(input, { target: { value: 'supplier' } })
  fireEvent.keyDown(input, { key: 'ArrowDown' })
  expect(changed).toHaveBeenCalledOnce()
  expect(key.mock.calls[0][0].key).toBe('ArrowDown')
  fireEvent.click(screen.getByRole('button', { name: 'This folder' }))
  expect(scope).toHaveBeenCalledOnce()
  expect(changed).toHaveBeenCalledOnce()
})
