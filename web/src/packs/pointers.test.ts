import { describe, expect, it } from 'vitest'
import {
  child,
  elementIdFor,
  parentPointers,
  parsePointer,
  pointer,
  pointerFromHash,
  valueAt
} from './pointers'

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

describe('an address that is not an address names nothing', () => {
  // RFC 6901 admits exactly two escapes. `replaceAll` left every other `~`
  // alone, so `~2` and a trailing bare `~` parsed as ordinary characters and
  // named a member nobody wrote — silently, which is the whole danger.
  it.each(['/~2', '/~', '/a~', '/~x', '/rules/~9/id', '/~~'])(
    'refuses the illegal escape in %s',
    (bad) => {
      expect(parsePointer(bad)).toBeUndefined()
    }
  )

  it('still accepts the two escapes that exist', () => {
    expect(parsePointer('/~0')).toEqual(['~'])
    expect(parsePointer('/~1')).toEqual(['/'])
    expect(parsePointer('/a~1b~0c')).toEqual(['a/b~c'])
    expect(parsePointer('/~01')).toEqual(['~1'])
  })

  it('refuses an illegal escape arriving through a fragment', () => {
    expect(valueAt({ '~2': 1 }, pointerFromHash('#/~2') ?? '/~2')).toBeUndefined()
  })
})

describe('valueAt, the one evaluator', () => {
  const document = {
    rules: [{ id: 'r0' }, { id: 'r1' }],
    title: 'a pack',
    'a/b': 1,
    'a~b': 2
  }

  it('reads what the address names', () => {
    expect(valueAt(document, '')).toBe(document)
    expect(valueAt(document, '/title')).toBe('a pack')
    expect(valueAt(document, '/rules/0/id')).toBe('r0')
    expect(valueAt(document, '/rules/1/id')).toBe('r1')
    expect(valueAt(document, '/a~1b')).toBe(1)
    expect(valueAt(document, '/a~0b')).toBe(2)
  })

  it.each(['/rules/01', '/rules/1e0', '/rules/-0', '/rules/', '/rules/+1', '/rules/1.0', '/rules/ 1'])(
    'refuses %s rather than selecting a real element',
    (bad) => {
      // `Number(part)` took every one of these for an index, so `/rules/01`
      // and `/rules/1e0` both selected rule one and `/rules/` selected rule
      // zero. An address either names something or names nothing.
      expect(valueAt(document, bad)).toBeUndefined()
    }
  )

  it('bounds-checks an index rather than reading past the end', () => {
    expect(valueAt(document, '/rules/2')).toBeUndefined()
    expect(valueAt(document, '/rules/9')).toBeUndefined()
  })

  it.each(['/constructor', '/toString', '/__proto__', '/rules/0/hasOwnProperty'])(
    'refuses %s, which is in no JSON document',
    (inherited) => {
      // `part in value` consults the prototype chain, so the Inspector
      // selected properties the document does not have and rendered them as
      // though it did.
      expect(valueAt(document, inherited)).toBeUndefined()
    }
  )

  it('reads an own member that happens to share a prototype name', () => {
    // The rule is "own property", not "not that name": a document may declare
    // `constructor` and it is then a member like any other.
    const declared = JSON.parse('{"constructor":{"x":1}}') as unknown
    expect(valueAt(declared, '/constructor/x')).toBe(1)
  })
})
