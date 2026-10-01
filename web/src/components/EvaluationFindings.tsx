import { msg, useLocale } from '../i18n'
import type { Evaluation, TraceEntry, TypeMismatch, UnknownCause } from '../mcp/types'
import { Section } from './primitives'

function jsonType(type: string): string {
  switch (type) {
    case 'string': return msg('text')
    case 'boolean': return msg('true or false')
    case 'number': return msg('number')
    case 'array': return msg('list')
    case 'object': return msg('object')
    case 'null': return msg('null')
    default: return type
  }
}
const pointer = (path: string) => path === '' ? msg('the whole document') : path
function scope(within: string | undefined): string {
  return within === undefined ? '' : ' ' + msg('Within collection {{path}}.', {path: pointer(within)})
}
function mismatchText(value: TypeMismatch): string {
  return msg('Fact {{path}} is {{factType}}; the comparison uses {{operandTypes}}. These JSON types cannot be equal.', {
    path: pointer(value.path), factType: jsonType(value.factType), operandTypes: value.operandTypes.map(jsonType).join(', '),
  }) + scope(value.within)
}
function causeText(value: UnknownCause): string {
  const values = {path: pointer(value.path ?? ''), factType: jsonType(value.factType ?? ''), requirement: value.evidenceRequirement}
  let text: string
  switch (value.cause) {
    case 'absent': text = msg('No value was supplied for fact {{path}}.', values); break
    case 'not-comparable': text = msg('Fact {{path}} is {{factType}}; ordered comparisons require a decimal string.', values); break
    case 'not-an-array': text = msg('Fact {{path}} is {{factType}}; this condition requires a list.', values); break
    case 'unknown': text = msg('Evidence {{requirement}} has unknown availability.', values); break
    case 'unsupported': text = msg('The evaluator does not support this condition.'); break
    default: text = msg('Unknown cause: {{cause}}.', {cause: value.cause})
  }
  return text + scope(value.within)
}

/** Render reported findings only; they never override the runtime's answer. */
export function ConditionFindings({entry}: {entry: Pick<TraceEntry, 'typeMismatches' | 'unknownCauses'>}) {
  useLocale()
  return <>
    {entry.typeMismatches?.map((value, index) => <p className="note note-warn" key={'type-' + index}>{mismatchText(value)}</p>)}
    {entry.unknownCauses?.map((value, index) => <p className="note" key={'unknown-' + index}>{causeText(value)}</p>)}
  </>
}

export function EvaluationFindings({payload}: {payload: Evaluation}) {
  useLocale()
  const entries = (payload.trace ?? []).filter(entry => entry.typeMismatches?.length || payload.disposition.kind === 'unresolved' && entry.unknownCauses?.length)
  return <>
    {entries.length > 0 && <Section title={msg('Input findings')}>
      {entries.map((entry, index) => <div key={index}>
        <p><code>{entry.id ?? entry.stage}</code></p>
        <ConditionFindings entry={{typeMismatches: entry.typeMismatches, unknownCauses: payload.disposition.kind === 'unresolved' ? entry.unknownCauses : undefined}} />
      </div>)}
    </Section>}
    {!!payload.unmetEvidence?.length && <Section title={msg('Unmet required evidence')}>
      <ul>{payload.unmetEvidence.map((item, index) => <li key={index}><code>{item.requirement}</code>{' — '}{item.state === 'absent' ? msg('Required evidence is absent.') : item.state === 'unknown' ? msg('Required evidence availability is unknown.') : item.state}</li>)}</ul>
    </Section>}
  </>
}
