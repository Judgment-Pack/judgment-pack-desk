import type {
  Escalation,
  EvidenceRequirement,
  Exception,
  Outcome,
  PackDocument,
  Rule,
  Source
} from '../mcp/types'
import { Fields, Json, Pill, Section } from './primitives'

/**
 * A reading view of one pack document.
 *
 * The document's own conditions are shown as formatted JSON rather than
 * rendered into English: turning a condition into prose would state what the
 * policy means, and only the document says that. Every other member is shown
 * as the document carries it, and a member the document omits is simply absent
 * rather than filled in.
 */
export function PackSemanticView({ document: doc }: { document: PackDocument }) {
  const outcomesById = new Map(doc.outcomes?.map((o) => [o.id, o]) ?? [])

  return (
    <div className="semantic">
      <Section title="Decision">
        <Fields
          items={[
            ['Question', doc.decision?.question],
            ['Intent', doc.decision?.intent]
          ]}
        />
      </Section>

      <Section title="Outcomes" count={doc.outcomes?.length}>
        {doc.outcomes?.length ? (
          <ul className="cards">
            {doc.outcomes.map((outcome) => (
              <OutcomeCard
                key={outcome.id}
                outcome={outcome}
                isFallback={doc.fallbackOutcome === outcome.id}
              />
            ))}
          </ul>
        ) : null}
      </Section>

      <Section title="Applicability">
        {doc.applicability ? (
          <>
            <p className="note">The pack decides only where this holds.</p>
            <Json value={doc.applicability} />
          </>
        ) : (
          <p className="note">No applicability condition: the pack does not narrow its own scope.</p>
        )}
      </Section>

      <Section title="Evidence requirements" count={doc.evidenceRequirements?.length}>
        {doc.evidenceRequirements?.length ? (
          <ul className="cards">
            {doc.evidenceRequirements.map((requirement) => (
              <EvidenceCard key={requirement.id} requirement={requirement} />
            ))}
          </ul>
        ) : null}
      </Section>

      <Section title="Rules" count={doc.rules?.length}>
        {doc.rules?.length ? (
          <ol className="cards">
            {doc.rules.map((rule) => (
              <RuleCard key={rule.id} rule={rule} outcome={outcomesById.get(rule.outcome)} />
            ))}
          </ol>
        ) : null}
      </Section>

      <Section title="Exceptions" count={doc.exceptions?.length}>
        {doc.exceptions?.length ? (
          <ul className="cards">
            {doc.exceptions.map((exception) => (
              <ExceptionCard
                key={exception.id}
                exception={exception}
                outcome={exception.outcome ? outcomesById.get(exception.outcome) : undefined}
              />
            ))}
          </ul>
        ) : null}
      </Section>

      <Section title="Escalation">
        {doc.escalation ? <EscalationView escalation={doc.escalation} /> : null}
      </Section>

      <Section title="Sources" count={doc.sources?.length}>
        {doc.sources?.length ? (
          <ul className="cards">
            {doc.sources.map((source) => (
              <SourceCard key={source.id} source={source} />
            ))}
          </ul>
        ) : null}
      </Section>

      <Section title="Metadata">
        {doc.metadata ? (
          <Fields
            items={[
              ['Authors', doc.metadata.authors?.join(', ')],
              ['Created', doc.metadata.createdAt],
              ['License', doc.metadata.license],
              ['Required extensions', doc.metadata.requiredExtensions?.join(', ')]
            ]}
          />
        ) : null}
      </Section>

      <Section title="Extensions">
        {doc.extensions ? <Json value={doc.extensions} /> : null}
      </Section>
    </div>
  )
}

function OutcomeCard({ outcome, isFallback }: { outcome: Outcome; isFallback: boolean }) {
  return (
    <li className="card">
      <div className="card-head">
        <h3>{outcome.label}</h3>
        <code className="id">{outcome.id}</code>
        {isFallback && <Pill tone="strong">fallback</Pill>}
      </div>
      {outcome.description && <p>{outcome.description}</p>}
    </li>
  )
}

function EvidenceCard({ requirement }: { requirement: EvidenceRequirement }) {
  return (
    <li className="card">
      <div className="card-head">
        <h3>
          <code className="id">{requirement.id}</code>
        </h3>
        <Pill tone={requirement.required ? 'strong' : 'quiet'}>
          {requirement.required ? 'required' : 'optional'}
        </Pill>
        {requirement.kind && <Pill>{requirement.kind}</Pill>}
      </div>
      <p>{requirement.description}</p>
    </li>
  )
}

function RuleCard({ rule, outcome }: { rule: Rule; outcome?: Outcome }) {
  return (
    <li className="card">
      <div className="card-head">
        <h3>
          <code className="id">{rule.id}</code>
        </h3>
        <Pill tone="strong">→ {outcome?.label ?? rule.outcome}</Pill>
        <Pill tone="quiet">on unknown: {rule.onUnknown}</Pill>
      </div>
      <p>{rule.description}</p>
      <Json value={rule.when} label="when" />
      <Fields
        items={[
          ['Rationale', rule.rationale],
          ['Evidence', <RefList key="ev" refs={rule.evidenceRequirementRefs} />],
          ['Sources', <RefList key="src" refs={rule.sourceRefs} />]
        ]}
      />
    </li>
  )
}

function ExceptionCard({ exception, outcome }: { exception: Exception; outcome?: Outcome }) {
  return (
    <li className="card">
      <div className="card-head">
        <h3>
          <code className="id">{exception.id}</code>
        </h3>
        <Pill tone="strong">{exception.effect}</Pill>
        {exception.outcome && <Pill>→ {outcome?.label ?? exception.outcome}</Pill>}
        <Pill tone="quiet">on unknown: {exception.onUnknown}</Pill>
      </div>
      <p>{exception.description}</p>
      <Json value={exception.when} label="when" />
      <Fields
        items={[
          ['Target rule', exception.targetRule && <code>{exception.targetRule}</code>],
          ['Sources', <RefList key="src" refs={exception.sourceRefs} />]
        ]}
      />
    </li>
  )
}

function EscalationView({ escalation }: { escalation: Escalation }) {
  return (
    <div className="card">
      <Fields
        items={[
          [
            'Target',
            <>
              {escalation.target?.name} <Pill tone="quiet">{escalation.target?.kind}</Pill>
            </>
          ],
          [
            'Triggers',
            <RefList key="triggers" refs={escalation.triggers} />
          ],
          ['Message', escalation.message]
        ]}
      />
    </div>
  )
}

function SourceCard({ source }: { source: Source }) {
  return (
    <li className="card">
      <div className="card-head">
        <h3>{source.title}</h3>
        <code className="id">{source.id}</code>
      </div>
      <Fields
        items={[
          ['Publisher', source.publisher],
          ['Published', source.publishedAt],
          ['Locator', <Locator key="loc" kind={source.locator?.kind} value={source.locator?.value} />],
          ['Rights', source.rights]
        ]}
      />
      {source.citation && (
        <blockquote className="citation">
          <p>{source.citation.excerpt}</p>
          <cite>{source.citation.location}</cite>
        </blockquote>
      )}
    </li>
  )
}

function Locator({ kind, value }: { kind?: string; value?: string }) {
  if (!value) return null
  const isLink = kind === 'uri' && /^https?:\/\//.test(value)
  return (
    <>
      {isLink ? (
        <a href={value} target="_blank" rel="noreferrer noopener">
          {value}
        </a>
      ) : (
        <code>{value}</code>
      )}{' '}
      {kind && <Pill tone="quiet">{kind}</Pill>}
    </>
  )
}

function RefList({ refs }: { refs?: string[] }) {
  if (!refs?.length) return null
  return (
    <span className="refs">
      {refs.map((ref) => (
        <code key={ref} className="id">
          {ref}
        </code>
      ))}
    </span>
  )
}
