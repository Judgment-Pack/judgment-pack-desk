import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import type { Disposition, Evaluation, HandoffTarget } from '../mcp/types'
import { Fields, Json, Pill, Section, slug } from './primitives'
import { TracePanel } from './TracePanel'

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
  useLocale()
  return (
    <div className="evaluation">
      {payload.rehearsal && (
        <p className="note"><Message text={"<0/> This run was declared not a decision (ADR-0028): no audit record was appended and no reviewed set was consulted, and the payload says so in band with<1/><2/>."} slots={[<strong>{msg("Rehearsal.")}</strong>, ' ', <code>"rehearsal": true</code>]} /></p>
      )}
      <DispositionPanel
        disposition={payload.disposition}
        handoffTarget={payload.handoffTarget}
      />
      {payload.draftPrototype && (
        <Section title={msg("Draft-RFC prototype")}>
          <div className="card card-warn">
            <p>{payload.draftPrototype.note}</p>
            <Fields
              items={[
                [msg("RFC"), payload.draftPrototype.rfc],
                [msg("Status"), payload.draftPrototype.status],
                [msg("Operators"), payload.draftPrototype.operators?.join(', ')],
                [
                  msg("Pack valid under the named specVersion"),
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
  useLocale()
  const reasons = disposition.reasons ?? []
  const triggeredBy = disposition.handoff?.triggeredBy ?? []
  return (
    <Section title={msg("Disposition")}>
      <p className="note">{msg("The portable JPS Core §8.3 disposition: the authoritative part of this payload. It authorizes nothing and executes nothing.")}</p>
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
              [msg("Kind"), <code key="kind">{disposition.kind}</code>],
              [
                msg("Outcome id"),
                disposition.outcomeId ? <code key="oid">{disposition.outcomeId}</code> : undefined
              ],
              [
                msg("Reasons"),
                reasons.length ? (
                  <TokenList key="reasons" values={reasons} />
                ) : (
                  <span key="reasons" className="quiet">{msg("none")}</span>
                )
              ],
              [
                msg("Handoff state"),
                <code key="handoff">{disposition.handoff?.state}</code>
              ],
              [
                msg("Triggered by"),
                triggeredBy.length ? <TokenList key="trig" values={triggeredBy} /> : undefined
              ]
            ]}
          />
        </div>
        <aside className="disposition-aside">
          <h4>{msg("Handoff target")}</h4>
          {handoffTarget ? (
            <>
              <p className="target-name">{handoffTarget.name}</p>
              <Pill tone="quiet">{handoffTarget.kind}</Pill>
              <p className="note">{msg("Reported beside the disposition, not inside it: §8.3 keeps the target outside. It is what the pack configures. No delivery is observed.")}</p>
            </>
          ) : (
            <p className="note">{msg("The payload reports no handoff target beside this disposition.")}</p>
          )}
        </aside>
      </div>
    </Section>
  )
}

/**
 * The envelope: the facts about the run rather than about the answer.
 * conformanceClaimReference is shown as what it is — a locator for the file
 * that states this runtime's claim. The payload states no claim, and neither
 * does this panel.
 */
function EnvelopePanel({ payload }: { payload: Evaluation }) {
  useLocale()
  return (
    <Section title={msg("Envelope")}>
      {payload.experimental && (
        <p className="note note-warn"><Message text={"<0/> The payload carries<1/><2/>: this surface may change or be removed without a compatibility promise."} slots={[<strong>{msg("Experimental surface.")}</strong>, ' ', <code>experimental: true</code>]} /></p>
      )}
      <div className="card">
        <Fields
          items={[
            [msg("Status"), <code key="status">{payload.status}</code>],
            [
              msg("Pack"),
              <span key="pack">
                <code>{payload.packId}</code> <Pill tone="strong">v{payload.packVersion}</Pill>
              </span>
            ],
            [
              msg("Pack declares specVersion"),
              <code key="spec">{payload.specVersion}</code>
            ],
            [
              msg("Evaluator contract"),
              <code key="eval">{payload.evaluatorSpecVersion}</code>
            ],
            [
              msg("Runtime"),
              payload.tool ? (
                <span key="tool">
                  <code>{payload.tool.name}</code> {payload.tool.version}
                </span>
              ) : undefined
            ],
            [msg("Command"), <code key="cmd">{payload.command}</code>],
            [msg("Output version"), payload.outputVersion],
            [
              msg("Bundled artifacts"),
              payload.artifact ? (
                <span key="artifact">
                  {payload.artifact.specVersion} · {payload.artifact.provenance} ·{' '}
                  <code>{payload.artifact.bundleDigest}</code>
                </span>
              ) : undefined
            ],
            [
              msg("Conformance claim"),
              <span key="claim"><Message text={"stated in <0/><1/><2/>"} slots={[<code>{payload.conformanceClaimReference}</code>, ' ', <span className="quiet">{msg("— a locator for the repository file that makes the claim. This payload makes none, and whatever that file claims is about the runtime, not about this pack, these facts, or whether acting on the disposition is correct.")}</span>]} /></span>
            ]
          ]}
        />
      </div>
    </Section>
  )
}

function TokenList({ values }: { values: string[] }) {
  useLocale()
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

/** The raw payload, for reading exactly what the runtime returned. */
export function EvaluationRaw({ raw }: { raw: string }) {
  useLocale()
  return <Json value={safeParse(raw)} />
}

function safeParse(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown
  } catch {
    return raw
  }
}
