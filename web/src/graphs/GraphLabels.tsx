import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { Pill } from '../components/primitives'
import type { GraphLabels as Labels } from './client'

/**
 * What the runtime says its graph payload is (ADR-0011, section 2), as it
 * said it: `kind`, `experimental`, `label`, `rehearsal` and
 * `conformanceClaimReference`, each only where the payload carries it. They are
 * the runtime's English, so they are marked as such. Desk's own word beside
 * them is "Experimental", which repeats the runtime's marker and claims
 * nothing.
 */
export function GraphLabels({ labels, withPill = true, plural = false }: { labels: Labels; withPill?: boolean; plural?: boolean }) {
  useLocale()
  const { kind, experimental, label, rehearsal, conformanceClaimReference: reference } = labels
  if (kind === undefined && experimental === undefined && label === undefined && rehearsal === undefined && reference === undefined) return null
  return <div className="graph-labels">
    <p className="meta">
      {withPill && <Pill tone="quiet">{msg('Experimental')}</Pill>}
      {kind !== undefined && <span><Message text={"kind <0/>"} slots={[<code lang="en" key="kind">{kind}</code>]} /></span>}
      {experimental !== undefined && <span><Message text={"experimental <0/>"} slots={[<code lang="en" key="experimental">{String(experimental)}</code>]} /></span>}
      {rehearsal !== undefined && <span><Message text={"rehearsal <0/>"} slots={[<code lang="en" key="rehearsal">{String(rehearsal)}</code>]} /></span>}
    </p>
    {label !== undefined && <p className="note" lang="en">{label}</p>}
    {reference !== undefined && <p><strong>{msg("Conformance claim")}</strong>{' '}<Message text={"stated in <0/><1/><2/>"} slots={[<code lang="en" key="reference">{reference}</code>, ' ', <span className="quiet" key="locator">{plural ? msg("— a locator for the repository file that makes the claim. This payload makes none, and whatever that file claims is about the runtime, not about these graphs' packs, these facts, or whether acting on the disposition is correct.") : msg("— a locator for the repository file that makes the claim. This payload makes none, and whatever that file claims is about the runtime, not about this graph's packs, these facts, or whether acting on the disposition is correct.")}</span>]} /></p>}
  </div>
}
