import { msg, systemMessage, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import type { RunState } from './run'

export function draftCheckStatus(state: RunState): {label:string;detail:string} {
  const check = state.candidates.at(-1)?.check
  if (state.checkRecovery === 'verifying') return {label:msg('Verifying checks…'),detail:msg('Verifying saved results and source proofs without running tests again.')}
  if (state.status === 'running' && (state.phase === 'check' || state.checkRun === 'running')) return {label:msg('Checking…'),detail:msg('Validating the draft and running its saved tests. No model call.')}
  if (state.checkRun === 'failed') return {label:msg('Check failed'),detail:systemMessage(state.detail)}
  if (state.checkRun === 'stopped') return {label:msg('Check interrupted'),detail:systemMessage(state.detail)}
  if (state.checkRecovery === 'outdated') return {label:msg('Checks outdated'),detail:systemMessage(state.checkIssue ?? state.detail)}
  if (state.checkRecovery === 'unavailable') return {label:msg('Checks unavailable'),detail:systemMessage(state.checkIssue ?? state.detail)}
  if (!check || state.restored) return {label:msg('Not checked'),detail:msg('Run checks from Pack options to validate this draft and its saved tests.')}
  if (!check.valid || check.cases.some(row => !row.passed)) return {label:msg('Checks failed'),detail:systemMessage(state.detail)}
  if (!check.cases.length) return {label:msg('Valid · no tests'),detail:msg('The draft is valid. Establish independent test cases before finalizing.')}
  return {label:msg('Checks passed'),detail:state.status === 'ready' ? msg('Validation and saved tests passed. Review the draft before finalizing.') : systemMessage(state.detail)}
}
export function DraftCheckStatus({state}:{state:RunState}) {
  useLocale()
  const status = draftCheckStatus(state)
  return <Tooltip content={status.detail}><span role="status" aria-label={msg('Draft checks')} tabIndex={0} className="quiet">{status.label}</span></Tooltip>
}
