import { useRef, useState } from 'react'
import { bindModelCall } from '@judgment-pack/harness/session'
import { msg, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { SettingsFeedback } from '../ui/SettingsFeedback'
import { probeCapabilities, type ProbeOutcome } from './capabilityProbe'
import { OVERRIDE_FLAGS, overrideProblem, probeOf, type EndpointDraft, type Tri } from './endpointDraft'
import { useAIConnectionScope } from './aiConnections'
import type { CapabilityFacts } from '../config/deskConfig'
import styles from './EndpointForm.module.css'

/** What a member the draft does not state falls back to, in words. */
const SOURCE = { probe: () => msg('probe'), override: () => msg('yours'), family: () => msg('provider default') } as const
/** The option for a flag the owner has not stated. */
const UNSTATED = 'default'
const YES_NO: Record<Tri, () => string> = { '': () => msg('Use the probe or the provider default'), yes: () => msg('Yes'), no: () => msg('No') }
const FLAG_LABEL: Record<(typeof OVERRIDE_FLAGS)[number], () => string> = {
  nativeToolCalls: () => msg('Native tool calls'),
  parallelToolCalls: () => msg('Parallel tool calls'),
  vision: () => msg('Images'),
  strictAlternation: () => msg('Strict turn order')
}
const MEMBER_LABEL: Record<string, () => string> = {
  contextWindow: () => msg('Context window'),
  outputReserve: () => msg('Output limit'),
  nativeToolCalls: FLAG_LABEL.nativeToolCalls,
  parallelToolCalls: FLAG_LABEL.parallelToolCalls,
  vision: FLAG_LABEL.vision,
  strictAlternation: FLAG_LABEL.strictAlternation,
  reasoningField: () => msg('Reasoning member')
}

/** One finding or override, as a reader says it. */
function stated(member: string, value: unknown): string {
  if (typeof value === 'boolean') return value ? msg('Yes') : msg('No')
  if (member === 'contextWindow' || member === 'outputReserve') return msg('{{tokens}} tokens', { tokens: Number(value) })
  return String(value)
}

/**
 * What the assistant will assume about this endpoint: the probe's finding, each
 * member the owner overrides laid over it, and where each came from. A member
 * neither states is the provider's default, which the harness fills in.
 */
export function EffectiveCapabilities({ draft }: { draft: EndpointDraft }) {
  useLocale()
  const probe = draft.probe !== null && draft.probe.of === probeOf(draft) ? draft.probe.facts : {}
  const override = overrideFacts(draft)
  const rows = Object.keys(MEMBER_LABEL).flatMap((member) => {
    const fromOverride = (override as Record<string, unknown>)[member]
    const fromProbe = (probe as Record<string, unknown>)[member]
    if (fromOverride !== undefined) return [{ member, value: fromOverride, source: SOURCE.override() }]
    if (fromProbe !== undefined) return [{ member, value: fromProbe, source: SOURCE.probe() }]
    return []
  })
  if (rows.length === 0) return <p className="quiet">{msg('Nothing is stated, so the provider\'s defaults apply.')}</p>
  return (
    <table className={styles.capabilities} aria-label={msg('What the assistant will assume')}>
      <tbody>
        {rows.map(({ member, value, source }) => (
          <tr key={member}><th scope="row">{MEMBER_LABEL[member]!()}</th><td>{stated(member, value)}</td><td>{source}</td></tr>
        ))}
      </tbody>
    </table>
  )
}

/** The overrides a draft states, as facts (what `effectiveFacts` would read back from its write). */
function overrideFacts(draft: EndpointDraft): CapabilityFacts {
  const facts: Record<string, unknown> = { ...draft.overrideHeld }
  const window = draft.override.contextWindow.trim(), reserve = draft.override.outputReserve.trim()
  if (/^\d+$/.test(window)) facts.contextWindow = Number(window)
  if (/^\d+$/.test(reserve)) facts.outputReserve = Number(reserve)
  for (const flag of OVERRIDE_FLAGS) if (draft.override[flag] !== '') facts[flag] = draft.override[flag] === 'yes'
  return facts as CapabilityFacts
}

/**
 * The capability record of a connection: a probe of the endpoint, each of its
 * findings the owner may override, the context window and output limit, and
 * what the assistant will then assume.
 *
 * **The probe is one press, on a saved endpoint**, and goes through the relay
 * with the connection's own saved address and key (`bindModelCall`), as every
 * other request to the endpoint does. It is offered for an OpenAI-compatible
 * endpoint, which is where models differ in what they take; the other two
 * protocols have the capabilities their reference documents. What it finds is
 * kept in the draft with the time it ran, written only when the form is saved,
 * and dropped the moment the draft names another endpoint or model.
 */
export function EndpointCapabilitiesSection({
  draft,
  onChange,
  canProbe,
  whyNot,
  problems
}: {
  draft: EndpointDraft
  onChange: (next: EndpointDraft) => void
  /** The probe may be pressed: the endpoint is saved, its key is in place and nothing else is in flight. */
  canProbe: boolean
  whyNot: string
  problems: Record<string, string | undefined>
}) {
  useLocale()
  const scope = useAIConnectionScope()
  const [asking, setAsking] = useState(false)
  const [outcome, setOutcome] = useState<ProbeOutcome | undefined>(undefined)
  const generation = useRef(0)
  const typed = overrideProblem(draft.override)
  const model = draft.model.trim() || draft.models[0] || ''
  const probeable = draft.kind === 'openai-compatible' && model !== ''

  const probe = async () => {
    const mine = ++generation.current
    setAsking(true)
    setOutcome(undefined)
    const call = bindModelCall('openai-compatible', scope ? { connectionId: scope.connection.id, connectionRevision: scope.connection.revision } : undefined)
    const answer = await probeCapabilities(call, model)
    if (generation.current !== mine) return
    setAsking(false)
    setOutcome(answer)
    if (answer.ok) onChange({ ...draft, probe: { at: answer.findings.at, facts: answer.findings.facts, of: probeOf({ ...draft, model }) } })
  }

  const setFlag = (flag: (typeof OVERRIDE_FLAGS)[number], value: Tri) => onChange({ ...draft, override: { ...draft.override, [flag]: value } })
  const found = draft.probe !== null && draft.probe.of === probeOf(draft) ? draft.probe : null

  return (
    <>
      {draft.kind === 'openai-compatible' && (
        <div className={styles.probe}>
          <Button disabled={!canProbe || !probeable || asking} onClick={() => void probe()}>
            {asking ? msg('Probing the endpoint…') : msg('Probe the endpoint')}
          </Button>
          <SettingsFeedback>
            {asking ? msg('Sending one small request…')
              : outcome !== undefined && !outcome.ok ? (outcome.problem === 'unreachable' ? msg('The endpoint could not be reached.') : outcome.problem === 'refused' ? msg('The endpoint refused the probe ({{status}}).', { status: outcome.status ?? 0 }) : msg('The endpoint answered with something the probe could not read.'))
              : found !== null ? msg('Probed {{when}}.', { when: found.at })
              : !probeable ? msg('Choose a default model to probe it.')
              : !canProbe ? whyNot
              : msg('Not probed.')}
          </SettingsFeedback>
        </div>
      )}
      <Field label={msg('Context window (tokens)')} hint={msg('Leave blank to use the probe or the provider default.')} error={typed.contextWindow ?? problems.contextWindow}>
        {(wiring) => <Input {...wiring} inputMode="numeric" spellCheck={false} value={draft.override.contextWindow} onChange={(event) => onChange({ ...draft, override: { ...draft.override, contextWindow: event.target.value } })} />}
      </Field>
      <Field label={msg('Output limit (tokens)')} error={typed.outputReserve ?? problems.outputReserve}>
        {(wiring) => <Input {...wiring} inputMode="numeric" spellCheck={false} value={draft.override.outputReserve} onChange={(event) => onChange({ ...draft, override: { ...draft.override, outputReserve: event.target.value } })} />}
      </Field>
      {OVERRIDE_FLAGS.map((flag) => (
        <Field key={flag} label={FLAG_LABEL[flag]()} error={problems[flag]}>
          {(wiring) => (
            // A select's option is never spelled `""` (see `ui/Select`), so "not stated" is offered as `default`.
            <Select {...wiring} value={draft.override[flag] === '' ? UNSTATED : draft.override[flag]} onValueChange={(value) => setFlag(flag, value === UNSTATED ? '' : (value as Tri))}
              options={([UNSTATED, 'yes', 'no'] as const).map((value) => ({ value, label: YES_NO[value === UNSTATED ? '' : value]() }))} />
          )}
        </Field>
      ))}
      <EffectiveCapabilities draft={draft} />
    </>
  )
}
