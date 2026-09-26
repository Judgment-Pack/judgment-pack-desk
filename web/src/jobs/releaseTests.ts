import { readFile } from '../files/client'
import { readTests } from '../packs/test-workspace/store'
import { emptySuite, executable, importMatrix, matrix } from '../packs/test-workspace/model'

/** Read saved expectations, never previous run results or unsaved AI proposals.
 * Match Tests' one-time matrix import without mutating its authoring store. */
export async function readReleaseTests(packKey: string, matrixPath?: string) {
  const stored = await readTests()
  const suite = Object.hasOwn(stored.content.suites, packKey) ? stored.content.suites[packKey]! : emptySuite()
  const cases = suite.cases.filter(c => !suite.deleted?.includes(c.id))
  if (matrixPath && !suite.recovered.includes('matrix:' + matrixPath)) {
    const file = await readFile(matrixPath)
    const imported = importMatrix(JSON.parse(file.content))
    cases.push(...imported.filter(c => !cases.some(held => held.id === c.id) && !suite.deleted?.includes(c.id)))
  }
  const eligible = cases.filter(executable)
  return {
    project: stored.project,
    matrix: eligible.length ? JSON.stringify(matrix(eligible)) : undefined,
    testSource: {
      packKey, suiteRevision: suite.revision,
      caseNames: Object.fromEntries(eligible.map(c => [c.id, c.name])),
      exploratoryCount: cases.length - eligible.length,
    },
  }
}
