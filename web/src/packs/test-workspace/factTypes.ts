import type { ProposalFinding } from '../../assistant/proposalWorkflow'
import { object } from './model'

export interface IntentionalFactType { caseId: string; path: string; within?: string; reason: string }
export interface FactTypeFinding extends ProposalFinding {
  operator: string; factType: string; expectedTypes: string[]; within?: string
}
export interface FactCheck { index: number; id: string; status: 'complete' | 'unavailable'; findings: FactTypeFinding[]; message?: string }
export function factCheckRows(report: Record<string, unknown>, document: unknown): FactCheck[] {
  if (report.factCheckVersion !== '1' || !Array.isArray(report.factChecks))
    throw Error('Fact type checks are unavailable. Update the connected runtime before proposing or saving AI test cases.')
  const cases = object(document) && Array.isArray(document.cases) ? document.cases : []
  if (report.factChecks.length !== cases.length) throw Error('The runtime did not check every case fact type.')
  for (const [index, row] of report.factChecks.entries()) {
    if (!object(row) || row.index !== index || !object(cases[index]) || row.id !== cases[index].id ||
      !['complete', 'unavailable'].includes(String(row.status)) || !Array.isArray(row.findings) ||
      (row.status === 'unavailable' && (typeof row.message !== 'string' || !row.message)) ||
      row.findings.some(f => !object(f) || f.code !== 'FACT-NOT-COMPARABLE' || typeof f.path !== 'string' ||
        typeof f.message !== 'string' || typeof f.operator !== 'string' || typeof f.factType !== 'string' ||
        (f.within !== undefined && typeof f.within !== 'string') || !Array.isArray(f.expectedTypes) ||
        !f.expectedTypes.length || f.expectedTypes.some(t => typeof t !== 'string')))
      throw Error('The runtime returned incomplete fact type findings.')
  }
  return report.factChecks as FactCheck[]
}
export function intentionalFactTypes(document: unknown): IntentionalFactType[] {
  if (!object(document) || document.intentionalFactTypes === undefined) return []
  const entries = document.intentionalFactTypes
  if (!Array.isArray(entries) || entries.length > 1024 || entries.some(entry => !object(entry) ||
    Object.keys(entry).some(key => !['caseId', 'path', 'within', 'reason'].includes(key)) ||
    typeof entry.caseId !== 'string' || !entry.caseId || typeof entry.path !== 'string' ||
    (entry.within !== undefined && typeof entry.within !== 'string') ||
    typeof entry.reason !== 'string' || !entry.reason.trim() || entry.reason.length > 2000))
    throw Error('Describe each intentional fact type test with a caseId, fact path and nonempty reason.')
  return entries as IntentionalFactType[]
}
const samePath = (entry: IntentionalFactType, row: FactCheck, finding: FactTypeFinding) =>
  entry.caseId === row.id && entry.path === finding.path && entry.within === finding.within
export function factTypeReviewFindings(rows: FactCheck[], document: unknown): ProposalFinding[] {
  let intentional: IntentionalFactType[]
  try { intentional = intentionalFactTypes(document) }
  catch (e) { return [{code:'FACT-TYPE-REVIEW', path:'/intentionalFactTypes', message:(e as Error).message}] }
  const errors: ProposalFinding[] = []
  for (const row of rows) {
    if (row.status !== 'complete') { errors.push({code:'FACT-CHECK-UNAVAILABLE', path:`/cases/${row.index}/facts`, message:row.message!}); continue }
    for (const finding of row.findings) {
      if (intentional.some(entry => samePath(entry, row, finding))) continue
      errors.push({code:'FACT-TYPE-REVIEW', path:`/cases/${row.index}/facts${finding.path}`, message:`Case "${row.id}": ${finding.message}. Correct accidental encoding, or describe this deliberate type test in intentionalFactTypes with caseId, path${finding.within !== undefined ? ', within' : ''} and reason. Preserve independently established expectations; do not change null, false, zero or missing inputs to make a test pass.`})
    }
  }
  for (const [index, entry] of intentional.entries()) {
    if (!rows.some(row => row.findings.some(finding => samePath(entry, row, finding))) ||
      intentional.slice(0,index).some(prior => prior.caseId === entry.caseId && prior.path === entry.path && prior.within === entry.within))
      errors.push({code:'FACT-TYPE-REVIEW', path:`/intentionalFactTypes/${index}`, message:'Intentional type explanations must name a current finding exactly once.'})
  }
  return errors
}
