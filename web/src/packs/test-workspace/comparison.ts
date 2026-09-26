import { jsonIdentity } from '../../research/checkCandidate'
import type { TestRun, TestCase } from './model'
const input = (c: TestCase) => ({ facts: c.row.facts, evidence: c.row.evidenceAvailability, sources: c.sources, mappings: c.sourceMappings })
const expectation = (c: TestCase) => Object.fromEntries(Object.entries(c.row).filter(([key]) => key.startsWith('expected')))
export function compareRuns(before: TestRun, after: TestRun) {
  const oldRows = before.report?.packs?.flatMap(p => p.rows ?? []) ?? []
  const newRows = after.report?.packs?.flatMap(p => p.rows ?? []) ?? []
  return [...new Set([...before.cases.map(c => c.id), ...after.cases.map(c => c.id)])].map(id => {
    const a = before.cases.find(c => c.id === id), b = after.cases.find(c => c.id === id)
    return { id, name: b?.name ?? a?.name ?? id, before: oldRows.find(r => r.id === id), after: newRows.find(r => r.id === id),
      presence: !a ? 'added' : !b ? 'removed' : 'both',
      inputsChanged: !!a && !!b && jsonIdentity(input(a)) !== jsonIdentity(input(b)),
      expectationChanged: !!a && !!b && jsonIdentity(expectation(a)) !== jsonIdentity(expectation(b)),
    }
  })
}
