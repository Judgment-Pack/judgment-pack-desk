import type { Disposition, Evaluation, HandoffTarget, TraceEntry } from '../mcp/types'
import { Fields, Json, Pill, Section } from './primitives'

/**
 * A reading view of one evaluation payload.
 *
 * Three things are kept apart on purpose. The **disposition** is the portable
 * JPS Core §8.3 answer and the authoritative part of the payload; it is shown
 * first and framed as such. The **handoff target** is reported beside the
 * disposition and not inside it, because §8.3 keeps it outside — so it is shown
 * beside, and no delivery is claimed. The **trace** is informative: it is what
 * the evaluator walked, kept so that an unknown resolution ignored stays
 * visible, and it decides nothing.
 *
 * Every value below is one the payload carries. A member the payload omits is
 * absent here rather than filled in, and a verdict string is shown as the
 * payload spells it rather than restated.
 */
export function EvaluationView({ payload }: { payload: Evaluation }) {
  return (
    <div className="evaluation">
      {payload.rehearsal && (
        <p className="note">
          <strong>Rehearsal.</strong> This run was declared not a decision
          (ADR-0028): no audit record was appended and no reviewed set was
          consulted, and the payload says so in band with{' '}
          <code>"rehearsal": true</code>.
        </p>
      )}
      <DispositionPanel
        disposition={payload.disposition}
        handoffTarget={payload.handoffTarget}
      />
      {payload.draftPrototype && (
        <Section title="Draft-RFC prototype">
          <div className="card card-warn">
            <p>{payload.draftPrototype.note}</p>
            <Fields
              items={[
                ['RFC', payload.draftPrototype.rfc],
                ['Status', payload.draftPrototype.status],
                ['Operators', payload.draftPrototype.operators?.join(', ')],
                [
                  'Pack valid under the named specVersion',
                  String(payload.draftPrototype.packValidUnderSpecVersion)
                ]
              ]}
            />
          </div>
        </Section>
      )}
      <TracePanel trace={payload.trace} />
      <EnvelopePanel payload={payload} />
    </div>
  )
}

/**
 * The disposition, and the handoff target reported beside it. Kind, outcome id,
 * reasons and handoff are the four members §8.3 admits; nothing else belongs
 * inside this frame.
 */
function DispositionPanel({
  disposition,
  handoffTarget
}: {
  disposition: Disposition
  handoffTarget?: HandoffTarget
}) {
  const reasons = disposition.reasons ?? []
  const triggeredBy = disposition.handoff?.triggeredBy ?? []
  return (
    <Section title="Disposition">
      <p className="note">
        The portable JPS Core §8.3 disposition: the authoritative part of this
        payload. It authorizes nothing and executes nothing.
      </p>
      <div className="disposition">
        <div className="disposition-main">
          <div className="card-head">
            <h3>
              <span className={`kind kind-${slug(disposition.kind)}`}>{disposition.kind}</span>
            </h3>
            {disposition.outcomeId && <Pill tone="strong">{disposition.outcomeId}</Pill>}
          </div>
          <Fields
            items={[
              ['Kind', <code key="kind">{disposition.kind}</code>],
              [
                'Outcome id',
                disposition.outcomeId ? <code key="oid">{disposition.outcomeId}</code> : undefined
              ],
              [
                'Reasons',
                reasons.length ? (
                  <TokenList key="reasons" values={reasons} />
                ) : (
                  <span key="reasons" className="quiet">
                    none
                  </span>
                )
              ],
              [
                'Handoff state',
                <code key="handoff">{disposition.handoff?.state}</code>
              ],
              [
                'Triggered by',
                triggeredBy.length ? <TokenList key="trig" values={triggeredBy} /> : undefined
              ]
            ]}
          />
        </div>
        <aside className="disposition-aside">
          <h4>Handoff target</h4>
          {handoffTarget ? (
            <>
              <p className="target-name">{handoffTarget.name}</p>
              <Pill tone="quiet">{handoffTarget.kind}</Pill>
              <p className="note">
                Reported beside the disposition, not inside it: §8.3 keeps the
                target outside. It is what the pack configures. No delivery is
                observed.
              </p>
            </>
          ) : (
            <p className="note">
              The payload reports no handoff target beside this disposition.
            </p>
          )}
        </aside>
      </div>
    </Section>
  )
}

/** One run of consecutive trace entries sharing a stage, keeping their place. */
interface TraceStage {
  stage: string
  /** Zero-based position of the first entry in the whole trace. */
  offset: number
  entries: TraceEntry[]
}

/**
 * Split the trace into consecutive same-stage runs. The order is the
 * evaluator's own walk, so entries are never regrouped across it: a stage that
 * appears twice appears twice here, and the numbering stays the trace's.
 */
function stagesOf(trace: TraceEntry[]): TraceStage[] {
  const stages: TraceStage[] = []
  trace.forEach((entry, index) => {
    const open = stages[stages.length - 1]
    if (open && open.stage === entry.stage) {
      open.entries.push(entry)
      return
    }
    stages.push({ stage: entry.stage, offset: index, entries: [entry] })
  })
  return stages
}

/** The trace as the staged walk it is, in the order the payload carries it. */
function TracePanel({ trace }: { trace: TraceEntry[] }) {
  const entries = trace ?? []
  return (
    <Section title="Trace" count={entries.length}>
      <p className="note">
        Informative: what the evaluator walked, in order. It decides nothing —
        the disposition above is the answer.
      </p>
      {entries.length === 0 ? (
        <p className="empty">This payload carries no trace entries.</p>
      ) : (
        <div className="trace">
          {stagesOf(entries).map((stage) => (
            <section className="trace-stage" key={`${stage.stage}:${stage.offset}`}>
              <h4>{stage.stage}</h4>
              <ol className="trace-list" start={stage.offset + 1}>
                {stage.entries.map((entry, index) => (
                  <TraceRow key={`${entry.id ?? stage.stage}:${stage.offset + index}`} entry={entry} />
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
    </Section>
  )
}

function TraceRow({ entry }: { entry: TraceEntry }) {
  return (
    <li className={entry.skipped ? 'trace-entry trace-entry-skipped' : 'trace-entry'}>
      <span
        className={`verdict verdict-${slug(entry.condition)}`}
        title={`condition: ${entry.condition}`}
      >
        {entry.condition}
      </span>
      <span className="trace-id">
        {entry.id ? (
          <code className="id">{entry.id}</code>
        ) : (
          <em className="quiet">unnamed {entry.stage} condition</em>
        )}
      </span>
      <span className="trace-badges">
        {entry.effect && <Pill>{entry.effect}</Pill>}
        {entry.outcome && <Pill tone="strong">→ {entry.outcome}</Pill>}
        {entry.skipped && <Pill>skipped</Pill>}
        {entry.suppressed && <Pill>suppressed</Pill>}
        {entry.onUnknown && <Pill tone="quiet">on unknown: {entry.onUnknown}</Pill>}
      </span>
    </li>
  )
}

/**
 * The envelope: the facts about the run rather than about the answer.
 * conformanceClaimReference is shown as what it is — a locator for the file
 * that states this runtime's claim. The payload states no claim, and neither
 * does this panel.
 */
function EnvelopePanel({ payload }: { payload: Evaluation }) {
  return (
    <Section title="Envelope">
      {payload.experimental && (
        <p className="note note-warn">
          <strong>Experimental surface.</strong> The payload carries{' '}
          <code>experimental: true</code>: this surface may change or be removed
          without a compatibility promise.
        </p>
      )}
      <div className="card">
        <Fields
          items={[
            ['Status', <code key="status">{payload.status}</code>],
            [
              'Pack',
              <span key="pack">
                <code>{payload.packId}</code> <Pill tone="strong">v{payload.packVersion}</Pill>
              </span>
            ],
            [
              'Pack declares specVersion',
              <code key="spec">{payload.specVersion}</code>
            ],
            [
              'Evaluator contract',
              <code key="eval">{payload.evaluatorSpecVersion}</code>
            ],
            [
              'Runtime',
              payload.tool ? (
                <span key="tool">
                  <code>{payload.tool.name}</code> {payload.tool.version}
                </span>
              ) : undefined
            ],
            ['Command', <code key="cmd">{payload.command}</code>],
            ['Output version', payload.outputVersion],
            [
              'Bundled artifacts',
              payload.artifact ? (
                <span key="artifact" title={payload.artifact.bundleDigest}>
                  {payload.artifact.specVersion} · {payload.artifact.provenance} · sha256{' '}
                  {payload.artifact.bundleDigest.slice(0, 12)}…
                </span>
              ) : undefined
            ],
            [
              'Conformance claim',
              <span key="claim">
                stated in <code>{payload.conformanceClaimReference}</code>{' '}
                <span className="quiet">
                  — a locator for the repository file that makes the claim. This
                  payload makes none, and whatever that file claims is about the
                  runtime, not about this pack, these facts, or whether acting on
                  the disposition is correct.
                </span>
              </span>
            ]
          ]}
        />
      </div>
    </Section>
  )
}

function TokenList({ values }: { values: string[] }) {
  return (
    <span className="refs">
      {values.map((value) => (
        <code key={value} className="id">
          {value}
        </code>
      ))}
    </span>
  )
}

/** The payload's own vocabularies are open; anything unrecognised styles neutral. */
function slug(value: string | undefined): string {
  if (!value) return 'other'
  return /^[a-z0-9-]+$/.test(value) ? value : 'other'
}

/** The raw payload, for reading exactly what the runtime returned. */
export function EvaluationRaw({ raw }: { raw: string }) {
  return <Json value={safeParse(raw)} />
}

function safeParse(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown
  } catch {
    return raw
  }
}
