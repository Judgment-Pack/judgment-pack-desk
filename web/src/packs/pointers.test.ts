import { describe, expect, it } from 'vitest'
import { child, elementIdFor, parentPointers, parsePointer, pointer, pointerFromHash } from './pointers'

describe('the pointer escaping', () => {
  it('matches the runtime’s own, byte for byte', () => {
    // internal/carrier/decode.go:242-261. `~` first, then `/`, and an empty
    // path is the empty string — not "/" and not undefined.
    expect(pointer([])).toBe('')
    expect(pointer(['rules', 0, 'when'])).toBe('/rules/0/when')
    expect(pointer(['a/b'])).toBe('/a~1b')
    expect(pointer(['a~b'])).toBe('/a~0b')
    expect(pointer(['~1'])).toBe('/~01')
  })

  it('round-trips a member name carrying both escapes', () => {
    const name = 'x~/y'
    expect(parsePointer(pointer([name]))).toEqual([name])
  })

  it('refuses a value that is not a pointer rather than repairing it', () => {
    expect(parsePointer('rules/0')).toBeUndefined()
    expect(parsePointer('#/rules/0')).toBeUndefined()
    expect(parsePointer('')).toEqual([])
  })
})

describe('the ancestor chain', () => {
  it('is longest-first and ends at the document', () => {
    expect(parentPointers('/rules/0/when/conditions/1')).toEqual([
      '/rules/0/when/conditions',
      '/rules/0/when',
      '/rules/0',
      '/rules',
      ''
    ])
  })

  it('is empty for the document itself, which has no ancestor', () => {
    expect(parentPointers('')).toEqual([])
  })
})

describe('the element id', () => {
  it('is the pointer verbatim, and is found by getElementById', () => {
    const block = document.createElement('div')
    block.id = elementIdFor('/rules/1')!
    block.setAttribute('data-pointer', '/rules/1')
    document.body.append(block)
    // The id and the attribute are one string, which is the point of there
    // being one function.
    expect(block.id).toBe(block.getAttribute('data-pointer'))
    expect(document.getElementById('/rules/1')).toBe(block)
    // And it is **not** a selector: `#/rules/1` is a type selector and a
    // class to a CSS parser, which is why nothing here uses querySelector.
    expect(() => document.querySelector('#/rules/1')).toThrow()
    block.remove()
  })

  it('claims no id for the document, whose pointer is the empty string', () => {
    expect(elementIdFor('')).toBeUndefined()
  })
})

describe('the fragment', () => {
  it('is decoded before it is read', () => {
    expect(pointerFromHash('#%2Frules%2F1')).toBe('/rules/1')
    expect(pointerFromHash('#/rules/1')).toBe('/rules/1')
  })

  it('names nothing where it is not a pointer or not valid encoding', () => {
    expect(pointerFromHash('#shortcuts')).toBeUndefined()
    expect(pointerFromHash('#%E0%A4%A')).toBeUndefined()
    expect(pointerFromHash('#')).toBeUndefined()
    expect(pointerFromHash('')).toBeUndefined()
  })
})

describe('one step down', () => {
  it('escapes the step, which is document data where it is an extension key', () => {
    expect(child('/extensions', 'example.pipeline')).toBe('/extensions/example.pipeline')
    expect(child('/extensions', 'a/b')).toBe('/extensions/a~1b')
    expect(child('/extensions', 'a~b')).toBe('/extensions/a~0b')
    expect(child('', 'rules')).toBe('/rules')
    expect(child('/rules', 0)).toBe('/rules/0')
  })

  it('refuses to build on something that is not a pointer', () => {
    expect(child('rules', 'when')).toBe('rules')
  })
})
