/**
 * The mode is an address, and the address is what the blocker reads.
 */
import { describe, expect, it } from 'vitest'
import { editShape, isEditing, withEditing, withShape } from './editMode'

const read = (search: string) => new URLSearchParams(search)
const spelled = (init: unknown) => new URLSearchParams(init as string).toString()

describe('reading the mode', () => {
  it('takes presence as the question, whatever the value is', () => {
    expect(isEditing(read(''))).toBe(false)
    expect(isEditing(read('?edit'))).toBe(true)
    expect(isEditing(read('?edit=1'))).toBe(true)
    expect(isEditing(read('?edit='))).toBe(true)
  })

  it('reads the shape, and calls anything but json a form', () => {
    expect(editShape(read('?edit=1'))).toBe('form')
    expect(editShape(read('?edit=1&shape=json'))).toBe('json')
    expect(editShape(read('?edit=1&shape=nonsense'))).toBe('form')
  })
})

describe('writing the mode', () => {
  it('keeps the rest of the address', () => {
    const params = read('?at=%2Frules%2F1')
    expect(spelled(withEditing(params, true))).toContain('at=%2Frules%2F1')
    expect(spelled(withEditing(params, true))).toContain('edit=1')
  })

  it('takes the shape out with the mode', () => {
    // A shape is a fact about editing. Leaving `shape=json` in the address of
    // a page nobody is editing is a record of a choice about nothing.
    const params = read('?edit=1&shape=json&at=%2Frules%2F1')
    const off = spelled(withEditing(params, false))
    expect(off).not.toContain('edit')
    expect(off).not.toContain('shape')
    expect(off).toContain('at=%2Frules%2F1')
  })

  it('writes the form shape as the absence of the parameter', () => {
    const params = read('?edit=1&shape=json')
    expect(spelled(withShape(params, 'form'))).toBe('edit=1')
    expect(spelled(withShape(params, 'json'))).toBe('edit=1&shape=json')
  })
})
