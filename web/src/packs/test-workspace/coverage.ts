import type { MatrixProbe } from '../../mcp/types'
import type { TestCase, TestRun, TestSuite } from './model'
import { executable, object } from './model'
import { jsonIdentity } from '../../research/checkCandidate'
import type { ProposalFinding } from '../../assistant/proposalWorkflow'

/** Coverage only describes the exact suite and pack that produced it. */
export function currentCoverage(run: TestRun, cases: TestCase[], digest: string): boolean {
  const current = cases.filter(executable)
  return !!digest && run.packDigest === digest && !run.error && !!run.report &&
    run.cases.length === current.length && current.every(c => {
      const held = run.cases.find(row => row.id === c.id)
      return held && jsonIdentity(held.row) === jsonIdentity(c.row)
    })
}
export function missingProbes(run: TestRun): MatrixProbe[] {
  return (run.report?.packs ?? []).flatMap(pack => pack.coverage ?? []).filter(p => p.status === 'missing')
}
export function latestCoverage(suite: TestSuite, digest: string) {
  return [...suite.runs].reverse().find(run => currentCoverage(run, suite.cases, digest))
}
export function missingTestsRequest(run: TestRun): string {
  return `Design additional test cases for these uncovered runtime probes. Keep every existing case unchanged and use new unique IDs. Ground expected results independently in the pack requirements and supplied sources. Do not copy an observed result into an expectation. Explain any probe you cannot cover without a policy decision. Cases need review and are not saved or run automatically.\nCOVERAGE RUN ${run.id}\nPACK DIGEST ${run.packDigest}\nMISSING PROBES\n${JSON.stringify(missingProbes(run))}`
}
/** Gap-filling never turns into an edit of existing expectations. */
export function additionFindings(document: unknown, existing: readonly string[]): ProposalFinding[] {
  if (!object(document) || !Array.isArray(document.cases)) return []
  return document.cases.flatMap((c, index) => object(c) && typeof c.id === 'string' && existing.includes(c.id)
    ? [{ code: 'EXISTING-CASE', path: `/cases/${index}/id`, message: 'Design missing tests must add a new case with a unique ID. Existing cases cannot be replaced.' }] : [])
}
