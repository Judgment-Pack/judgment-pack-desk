import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, it } from 'vitest'
import type { PackDocument } from '../mcp/types'
import { projectLogic } from './logicModel'
import { projectLogicGraph } from './logicGraph'
import { relationshipPositions } from '../components/RelationshipMap'

const fixture = (name: string): PackDocument => JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', `${name}.pack.json`), 'utf8'))
it('connects each rule to its declared outcome, with no category or documentary edges', () => {
  const doc = fixture('full'), graph = projectLogicGraph(projectLogic(doc))
  doc.rules.forEach((rule, i) => {
    const target = doc.outcomes.findIndex(outcome => outcome.id === rule.outcome)
    expect(graph.edges).toContainEqual(expect.objectContaining({ source: `/rules/${i}`, target: `/outcomes/${target}`, label: 'Contributes' }))
  })
  expect(graph.edges.some(edge => /evidence|sources|resolution|applicability/.test(edge.source + edge.target))).toBe(false)
})
it('keeps exclusion and forced-outcome edges exact even when grouping is enabled', () => {
  const doc = fixture('exceptions')
  const graph = projectLogicGraph(projectLogic(doc), true)
  doc.exceptions!.forEach((item, i) => {
    if (item.effect === 'suppress-rule') expect(graph.edges).toContainEqual(expect.objectContaining({ source: `/exceptions/${i}`, target: `/rules/${doc.rules.findIndex(rule => rule.id === item.targetRule)}`, label: 'Excludes' }))
    if (item.effect === 'force-outcome') expect(graph.edges).toContainEqual(expect.objectContaining({ source: `/exceptions/${i}`, target: `/outcomes/${doc.outcomes.findIndex(outcome => outcome.id === item.outcome)}`, label: 'Forces' }))
    if (item.effect === 'escalate') expect(graph.edges).toContainEqual(expect.objectContaining({ source: `/exceptions/${i}`, target: '/escalation', semantic: 'requests-handoff' }))
  })
})
it('groups large packs by exact outcome IDs, preserves order and expands real item pointers', () => {
  const doc = fixture('minimal')
  doc.rules = Array.from({ length: 80 }, (_, i) => ({ ...doc.rules[0]!, id: `rule-${i}` }))
  const model = projectLogic(doc), graph = projectLogicGraph(model, true)
  const group = graph.nodes.find(n => n.items.length === 80)!
  expect(group.items.map(item => item.pointer)).toEqual(doc.rules.map((_, i) => `/rules/${i}`))
  expect(graph.edges).toHaveLength(1)
  const expanded = projectLogicGraph(model, true, new Set([group.id]))
  expect(expanded.edges).toHaveLength(80)
  expect(expanded.nodes.filter(n => n.group.id === 'rules')).toHaveLength(80)
})
it('does not invent targets for unresolved references', () => {
  const doc = fixture('minimal'); doc.rules[0]!.outcome = 'absent'
  expect(projectLogicGraph(projectLogic(doc)).edges).toEqual([])
})
it('lays out variable-height nodes without overlap at enlarged text sizes', () => {
  const nodes = [{ id: 'long', column: 0 }, { id: 'next', column: 0 }, { id: 'outcome', column: 1 }]
  for (const unit of [16, 32]) {
    const sizes = { long: { width: 21 * unit, height: 1200 }, next: { width: 21 * unit, height: 80 } }
    const positions = relationshipPositions(nodes, sizes, unit)
    expect(positions.get('next')!.y).toBeGreaterThanOrEqual(positions.get('long')!.y + sizes.long.height + 2 * unit)
    expect(positions.get('outcome')!.x).toBeGreaterThan(positions.get('long')!.x + sizes.long.width)
  }
})

it('groups only special cases with the same forced outcome and preserves their exact members',()=>{
 const doc=fixture('minimal')
 doc.exceptions=[0,1].map(i=>({id:`force-${i}`,description:`Force case ${i}`,effect:'force-outcome',outcome:'proceed',when:{op:'literal',value:true},onUnknown:'escalate'}))
 const model=projectLogic(doc),graph=projectLogicGraph(model,true)
 const group=graph.nodes.find(n=>n.group.id==='exceptions')!
 expect(group.items.map(i=>i.pointer)).toEqual(['/exceptions/0','/exceptions/1'])
 expect(graph.edges.find(e=>e.source===group.id)).toMatchObject({target:'/outcomes/0',semantic:'forces'})
 const expanded=projectLogicGraph(model,true,new Set([group.id]))
 expect(expanded.nodes.filter(n=>n.group.id==='exceptions')).toHaveLength(2)
 expect(expanded.edges.filter(e=>e.semantic==='forces')).toHaveLength(2)
})

it('does not invent a handoff destination when none is declared',()=>{
 const doc=fixture('exceptions'); delete doc.escalation
 const graph=projectLogicGraph(projectLogic(doc))
 expect(graph.nodes.some(node=>node.group.id==='handoff')).toBe(false)
 expect(graph.edges.some(edge=>edge.semantic==='requests-handoff')).toBe(false)
})
