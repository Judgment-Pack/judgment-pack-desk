import { sha256 } from '@judgment-pack/harness/records'
import { restoreLedger } from '../../chat/checkpoint'
import { INITIAL_STATE } from '../../research/initialState'
import { Ledger } from '../../research/ledger'
import type { ResearchRunBinding } from '../../research/useResearchRun'
import type { PackDraft } from './model'

/** Decode retained history for display. This object owns no execution capability. */
export function historicalDraftBinding(artifact: PackDraft): ResearchRunBinding {
  const saved = artifact.checkpoint.state
  const ledger = new Ledger('historical-display')
  // Parsing an original response reconstructs display rows only. Verification
  // remains unchecked; neither acquisition nor a model can run from this view.
  restoreLedger(ledger, artifact.checkpoint.sources)
  return {
    run: null, ledger, sources: ledger.sources, model: '', researchConfigured: false,
    blocked: 'Open the linked chat to check this retained draft.',
    state: {
      ...INITIAL_STATE, ...saved, phase: 'review', status: 'needs-input', restored: true,
      readiness: '', tracedBasis: undefined, citations: [], verdicts: {}, registries: {},
      events: [], streaming: undefined, streamingId: undefined, execution: undefined,
      taskProgress: undefined, checkRecovery: 'unavailable', checkRun: undefined,
      candidates: saved.candidates.map(({check, ...candidate}) => ({
        ...candidate, digest: sha256(candidate.text), document: JSON.parse(candidate.text),
        previousCheck: check ?? candidate.previousCheck,
      })),
      expectationIssues: saved.expectationIssues.map(({proposal: _proposal, ...issue}) => issue),
    },
  }
}
