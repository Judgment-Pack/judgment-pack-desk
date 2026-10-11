import { useMemo } from 'react'
import { msg } from '../i18n'
import { useTestStorage } from '../packs/test-workspace/store'
import type { TestSuite } from '../packs/test-workspace/model'

/** Observations remain separate from expected results and never establish expectations. */
export function savedTestContext(suite: TestSuite, text: string): string {
  const run = suite.runs.at(-1)
  return JSON.stringify({
    cases: suite.cases.map(c => ({ id: c.id, name: c.name, revision: c.revision, inputsAndExpectations: c.row, rationale: c.rationale })),
    latestRun: run ? { at: run.at, matchesCurrentPack: run.packText === text,
      matchesCurrentCases: JSON.stringify(run.cases) === JSON.stringify(suite.cases),
      report: run.report, trial: run.trial, error: run.error } : null,
    instruction: 'Saved observations are not authority for expected results. Test changes require review in Tests.'
  })
}
export function usePackChatContext(owner: string, text: string | undefined, title: string, draft = false) {
  const { suite, query } = useTestStorage(owner)
  const tests = useMemo(() => query.isSuccess && text !== undefined ? savedTestContext(suite, text) : undefined, [suite, text, query.isSuccess])
  return text === undefined ? undefined : { text, title, kind: 'pack' as const,
    detail: draft ? msg('Draft pack') : msg('Pack'),
    tests }
}
