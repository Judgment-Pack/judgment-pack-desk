import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { projectLogic, selectedItem, matchingItems, itemTrace } from './logicModel'
import type { PackDocument } from '../mcp/types'

const fixture = (name: string): PackDocument => JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', `${name}.pack.json`), 'utf8'))
describe('one projection for the pack views', () => {
  it('keeps nested conditions and declared effects byte-independent and unaltered', () => {
    const doc = fixture('full'), model = projectLogic(doc)
    const rule = selectedItem(model, '/rules/1/when/conditions/0/value')!
    expect(rule.item.value).toBe(doc.rules[1])
    expect(rule.item.pointer).toBe('/rules/1')
    expect(model.groups.map(g => g.id)).toContain('resolution')
    expect(selectedItem(model, '/rules/01')).toBeUndefined()
    expect(selectedItem(model, '/constructor')).toBeUndefined()
  })
  it('keeps all exception kinds and locates the target of suppression', () => {
    const model = projectLogic(fixture('exceptions'))
    const exceptions = model.groups.find(g => g.id === 'exceptions')!
    expect(exceptions.items.map(i => i.effect)).toEqual(expect.arrayContaining([
      expect.stringContaining('Exclude rule '), expect.stringContaining('Force '), 'Request handoff'
    ]))
    expect(matchingItems(exceptions, 'decline-outside-window').length).toBeGreaterThan(0)
  })
  it('does not turn malformed members into an invented rule', () => {
    const doc = { ...fixture('minimal'), rules: [null, { id: 'unknown', effect: 'future-effect' }], exceptions: 'bad' } as unknown as PackDocument
    const model = projectLogic(doc)
    expect(model.groups.find(g => g.id === 'rules')!.items[0]!.label).toContain('Unrecognized')
    expect(model.groups.find(g => g.id === 'exceptions')!.items).toEqual([])
    expect(matchingItems(model.groups.find(g => g.id === 'rules')!, 'future-effect')).toHaveLength(1)
  })
  it('keeps skipped, suppressed, unknown and absent observations distinct from false', () => {
    const doc = fixture('minimal'), model = projectLogic(doc), group = model.groups.find(g => g.id === 'rules')!, item = group.items[0]!
    const id = doc.rules[0]!.id
    expect(itemTrace(group, item)).toBeUndefined()
    expect(itemTrace(group, item, [])).toBe('Unreported')
    expect(itemTrace(group, item, [{ stage: 'rule', id, condition: 'false' }])).toBe('Not met')
    expect(itemTrace(group, item, [{ stage: 'rule', id, condition: 'unknown' }])).toBe('Cannot determine')
    expect(itemTrace(group, item, [{ stage: 'rule', id, condition: 'not-evaluated', skipped: true }])).toBe('Not evaluated')
    expect(itemTrace(group, item, [{ stage: 'rule', id, condition: 'not-evaluated', suppressed: true }])).toBe('Not evaluated · excluded by a special case')
  })
})
