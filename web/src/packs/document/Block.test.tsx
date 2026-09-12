import { cleanup, fireEvent, render } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Block, CursorContext, SelectionContext } from './Block'

afterEach(() => { document.getSelection()?.removeAllRanges(); cleanup() })

it('leaves a text selection intact and selects the innermost block on an ordinary click', () => {
  const select = vi.fn(), move = vi.fn()
  const { container } = render(<SelectionContext.Provider value={{ at: null, select }}>
    <CursorContext.Provider value={{ at: '/description', move }}>
      <Block pointer="/decision"><Block pointer="/description" as="p">A complete description to copy.</Block></Block>
    </CursorContext.Provider>
  </SelectionContext.Provider>)
  const paragraph = container.querySelector('p')!
  const range = document.createRange()
  range.selectNodeContents(paragraph)
  document.getSelection()!.addRange(range)
  fireEvent.click(paragraph)
  expect(select).not.toHaveBeenCalled()
  expect(move).not.toHaveBeenCalled()
  expect(document.getSelection()!.toString()).toBe(paragraph.textContent)
  document.getSelection()!.removeAllRanges()
  fireEvent.click(paragraph)
  expect(select).toHaveBeenCalledExactlyOnceWith('/description')
  expect(move).toHaveBeenCalledExactlyOnceWith('/description')
})
