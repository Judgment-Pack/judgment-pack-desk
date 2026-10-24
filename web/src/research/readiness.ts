import {jsonIdentity} from './checkCandidate'
import type {RunState} from './run'

/** Exact candidate, independent cases and current source evidence comparison. */
export function readinessKey(state:RunState):string {
  return JSON.stringify([
    state.candidates.at(-1)?.digest ?? '',
    state.cases.map(row=>[row.id,jsonIdentity(row)]),
    state.citations.map(citation=>[citation.sourceId,citation.location,citation.traced]),
    state.tracedBasis ?? '',
  ])
}
