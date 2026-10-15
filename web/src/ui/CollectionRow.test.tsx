import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Link, MemoryRouter, useLocation } from 'react-router-dom'
import { CollectionRow } from './CollectionRow'

afterEach(() => { cleanup(); window.getSelection()?.removeAllRanges(); vi.restoreAllMocks() })
function Location() { return <output data-testid="location">{useLocation().pathname}</output> }

it('opens the primary destination from any data cell, keeping native row/link semantics', () => {
  render(<MemoryRouter><Location/><table><tbody><CollectionRow>
    <td><Link data-row-primary to="/dialects/one">Suppliers</Link></td><td><span>Updated today</span></td>
  </CollectionRow></tbody></table></MemoryRouter>)
  fireEvent.click(screen.getByText('Updated today'))
  expect(screen.getByTestId('location').textContent).toBe('/dialects/one')
  expect(document.activeElement).toBe(screen.getByRole('link', { name: 'Suppliers' }))
  expect(screen.getByRole('row').hasAttribute('tabindex')).toBe(false)
  expect(screen.queryByRole('button')).toBeNull()
})

it('activates a detail button once and leaves checkbox labels, secondary links and actions independent', () => {
  const open = vi.fn(), retry = vi.fn()
  render(<MemoryRouter><Location/><table><tbody><CollectionRow>
    <td><button data-row-primary onClick={open}><span>Record</span></button></td>
    <td>Ready</td><td><label><input type="checkbox"/>Select record</label></td>
    <td><button onClick={retry}>Retry</button><Link to="/jobs/one">View job</Link></td>
  </CollectionRow></tbody></table></MemoryRouter>)
  fireEvent.click(screen.getByText('Ready'))
  expect(open).toHaveBeenCalledTimes(1)
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Record' }))
  fireEvent.click(screen.getByText('Record'))
  expect(open).toHaveBeenCalledTimes(2)
  fireEvent.click(screen.getByText('Select record'))
  expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(retry).toHaveBeenCalledOnce()
  fireEvent.click(screen.getByRole('link', { name: 'View job' }))
  expect(screen.getByTestId('location').textContent).toBe('/jobs/one')
  expect(open).toHaveBeenCalledTimes(2)
})

it('preserves native link modifiers and middle-click, without opening button details for those gestures', () => {
  const clicks = vi.fn(), details = vi.fn(), openTab = vi.spyOn(window, 'open').mockReturnValue(null)
  render(<table><tbody><CollectionRow>
    <td><a data-row-primary href="/dialects/one" onClick={e=>{e.preventDefault();clicks(e.ctrlKey,e.metaKey,e.button)}} onAuxClick={e=>{e.preventDefault();clicks(e.ctrlKey,e.metaKey,e.button)}}>Input</a></td><td>Link area</td>
  </CollectionRow><CollectionRow><td><button data-row-primary onClick={details}>Details</button></td><td>Button area</td></CollectionRow></tbody></table>)
  fireEvent.click(screen.getByText('Link area'), { ctrlKey: true })
  fireEvent.click(screen.getByText('Link area'), { metaKey: true })
  fireEvent(screen.getByText('Link area'), new MouseEvent('auxclick', { bubbles:true, button:1 }))
  expect(clicks.mock.calls).toEqual([[true,false,0],[false,true,0]])
  expect(openTab).toHaveBeenCalledExactlyOnceWith(new URL('/dialects/one', location.href).href, '_blank', 'noopener')
  fireEvent.click(screen.getByText('Button area'), { ctrlKey: true })
  fireEvent(screen.getByText('Button area'), new MouseEvent('auxclick', { bubbles:true, button:1 }))
  expect(details).not.toHaveBeenCalled()
})

it('does not activate while selecting text, editing content, or using an excluded control area', () => {
  const open = vi.fn()
  render(<table><tbody><CollectionRow>
    <td><button data-row-primary onClick={open}>Details</button></td><td><span>Selectable text</span></td>
    <td><div contentEditable suppressContentEditableWarning>Editable value</div></td>
    <td data-row-ignore>Custom control</td><td onClick={e=>e.preventDefault()}>Prevented</td>
  </CollectionRow></tbody></table>)
  const text = screen.getByText('Selectable text'), range = document.createRange()
  range.selectNodeContents(text); window.getSelection()?.addRange(range)
  fireEvent.click(text)
  window.getSelection()?.removeAllRanges()
  fireEvent.click(screen.getByText('Editable value'))
  fireEvent.click(screen.getByText('Custom control'))
  fireEvent.click(screen.getByText('Prevented'))
  expect(open).not.toHaveBeenCalled()
})

it('keeps rows without a destination inert and respects disabled primary actions', () => {
  const open = vi.fn()
  render(<table><tbody><CollectionRow actionable={false}><td>Recorded event</td></CollectionRow>
    <CollectionRow><td><button data-row-primary disabled onClick={open}>Unavailable</button></td><td>No access</td></CollectionRow>
  </tbody></table>)
  fireEvent.click(screen.getByText('Recorded event'));fireEvent.click(screen.getByText('No access'))
  expect(open).not.toHaveBeenCalled()
  expect(screen.getAllByRole('row')[0]!.hasAttribute('data-row-action')).toBe(false)
})
