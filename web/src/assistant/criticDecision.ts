import type { CriticRecord } from '@judgment-pack/harness/criticSwitch'
import { msg as uiMessage } from '../i18n'

/** Presentation only: Agent owns the decision and its stable reason. */
export function criticDecisionLine(record: CriticRecord, msg = uiMessage): string {
  const status = record.ran === undefined ? msg('Review requested') : record.ran ? msg('Review ran') : msg('Review skipped')
  const reasons = {
    person: msg('Chat setting'), project: msg('Project setting'), table: msg('Policy table default'),
    'model-asked': msg('Assistant requested review'), cases: msg('Pack has tests'),
    'changes-existing': msg('Changes an existing pack'), 'no-proposal': msg('No pack proposal'),
    'reviewer-assigned': msg('Human reviewer assigned'), purpose: msg('Graph run'), phase: msg('Plan mode'),
    child: msg('Delegated task'), budget: msg('Request allowance exhausted'), off: msg('Review is off')
  }
  const reason = record.reason ?? record.trigger
  return [status, record.mode, reason && reasons[reason], record.reasoning && msg('Reasoning effort: {{tier}}', {tier: record.reasoning})].filter(Boolean).join(' · ')
}
