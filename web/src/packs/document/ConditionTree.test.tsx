/**
 * The condition tree, and the one thing it must never do.
 *
 * A paraphrase of a condition is a second statement of the policy, written by
 * a page rather than by whoever authored the pack. So the assertions here are
 * about words that must be present verbatim and words that must be absent.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import type { PackDocument } from '../../mcp/types'
import { ConditionTree } from './ConditionTree'

afterEach(cleanup)

const full = JSON.parse(
  readFileSync(join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'), 'utf8')
) as PackDocument

function draw(condition: unknown, at: string) {
  return render(
    <MemoryRouter>
      <ConditionTree condition={condition} at={at} />
    </MemoryRouter>
  )
}

describe('an ordered comparison', () => {
  it('keeps the operand’s quotes, because a decimal operand is a string', () => {
    // `"5000"` and `5000` are the difference between a document the runtime
    // accepts and one it refuses by name — the probe in the PR body shows
    // JPS-STRUCTURE-DECIMAL-OPERAND on exactly that edit.
    draw(full.rules[1]!.when, '/rules/1/when')
    expect(screen.getByText('"5000"')).toBeTruthy()
    expect(screen.queryByText('5000')).toBeNull()
  })

  it('keeps the document’s own operator word and never prose', () => {
    const { container } = draw(full.rules[1]!.when, '/rules/1/when')
    expect(screen.getByText('greater-than')).toBeTruthy()
    for (const prose of ['is greater than', 'greater than ', 'must be', 'at least']) {
      expect(container.textContent, prose).not.toContain(prose)
    }
  })
})

describe('the five node kinds', () => {
  it('renders all of them, each as the schema names it', () => {
    const { container } = draw(
      {
        op: 'all',
        conditions: [
          { op: 'fact', path: '/a', operator: 'in', value: ['x'] },
          { op: 'any', conditions: [{ op: 'not', condition: { op: 'literal', value: true } }] },
          { op: 'evidence-present', evidenceRequirement: 'screening-report' }
        ]
      },
      '/rules/0/when'
    )
    const text = container.textContent ?? ''
    for (const word of ['all of', 'any of', 'not', 'literal', 'evidence-present', 'in']) {
      expect(text, word).toContain(word)
    }
    expect(screen.getByText('["x"]')).toBeTruthy()
    expect(screen.getByText('true')).toBeTruthy()
  })

  it('prints a kind it has never seen rather than dropping it', () => {
    // A runtime may grow a sixth. A view that silently skipped it would show a
    // condition that is not the one on disk.
    const { container } = draw({ op: 'sometime-in-future', detail: 42 }, '/rules/0/when')
    expect(container.textContent).toContain('sometime-in-future')
    expect(container.textContent).toContain('42')
  })
})

describe('every node’s address', () => {
  it('reaches the operand itself', () => {
    const { container } = draw(full.rules[1]!.when, '/rules/1/when')
    const found = new Set(
      [...container.querySelectorAll('[data-pointer]')].map((element) =>
        element.getAttribute('data-pointer')
      )
    )
    for (const pointer of [
      '/rules/1/when',
      '/rules/1/when/conditions/0',
      '/rules/1/when/conditions/0/path',
      '/rules/1/when/conditions/0/operator',
      '/rules/1/when/conditions/0/value',
      '/rules/1/when/conditions/1',
      '/rules/1/when/conditions/1/conditions/1/condition'
    ]) {
      expect(found.has(pointer), pointer).toBe(true)
    }
    // And it is an element, which is what makes it a deep-link target and a
    // diagnostic anchor.
    expect(document.getElementById('/rules/1/when/conditions/0/value')).toBeTruthy()
  })
})
