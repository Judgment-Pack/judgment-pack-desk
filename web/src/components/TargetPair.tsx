/**
 * One reported handoff-target assertion, expected beside actual.
 *
 * Two surfaces carry this pair on the same vocabulary and the same semantics:
 * a pack matrix row (ADR-0025) and, since ADR-0032, a graph matrix row for the
 * composite and each named node's comparison for that node. One renderer for
 * both, because a second would be free to describe one surface's `unavailable`
 * differently from the other's, and they are the same word about the same
 * thing.
 *
 * **The members are display values and are never compared here.** Each is a
 * capped canonical rendering with a digest tail where it was too long, the
 * literal `null`, or `unavailable`. The comparator decided on *decoded*
 * targets, so a mark drawn from comparing these strings could contradict the
 * verdict the runtime reached — two renderings that differ only past the cap
 * would read as a difference that is not one. The row's own status is the only
 * verdict, and it is already shown beside the pair.
 *
 * **The three states stay three.** "No target" is an answer — the disposition
 * requested no handoff — and `unavailable` is the absence of one. Collapsing
 * them would turn a refused run into a policy that hands off nowhere.
 *
 * `unavailable` is reachable on **one** carrier only: a matrix row whose run
 * was refused where the row expected a disposition. A graph *node* comparison
 * exists only because the walk evaluated that node, so its actual value is
 * always a rendering or the literal `null` and never `unavailable`. This
 * renderer would show the word if a payload carried it — showing what the
 * runtime said is the rule — but nothing here manufactures it, and no fixture
 * should pretend a node can reach it.
 */
import { NO_HANDOFF_TARGET, describeHandoffTarget } from '../mcp/canonical'

/**
 * One side of the target pair, shown and never compared.
 *
 * `member` undefined is not a state the payload reports: the two members
 * appear together, so an absent one means this side was never rendered and is
 * said to be not reported rather than shown as no target.
 */
export function TargetSide({ label, member }: { label: string; member: string | undefined }) {
  return (
    <div className="row-side">
      <span className="row-side-label">{label}</span>
      <p className="target-name">
        {member === undefined ? '(not reported)' : describeHandoffTarget(member)}
      </p>
    </div>
  )
}

/**
 * Anything that may carry a reported handoff-target assertion: a pack matrix
 * row, a graph matrix row, or one graph node comparison.
 *
 * The members are flat optionals because the wire types mirror the JSON, and
 * the JSON carries them flat. The *rule* is that they appear together — see
 * `handoffTargetPair`, which is the one place that rule is applied.
 */
export interface HandoffTargetCarrier {
  expectedHandoffTarget?: string
  actualHandoffTarget?: string
}

/**
 * The one pair a carrier reports, or nothing where it asserts none.
 *
 * The runtime's rule is that the two members appear **together**, exactly when
 * a well-formed assertion rode a run this walk performed: a carrier asserting
 * nothing has neither, and one asserting "no target at all" has both, each the
 * literal `null`. A row whose *defect* was the problem — an undecodable
 * expectation, a node the graph does not declare — reports that defect in its
 * detail and no pair at all.
 *
 * One accessor applies the rule so that no call site re-derives it and gets it
 * half right. Reading presence off `expected` alone, as two call sites once
 * did, would render half a pair on a payload that reported only one member,
 * and half a pair reads as an assertion that was made and not answered.
 */
export function handoffTargetPair(
  carrier: HandoffTargetCarrier
): { expected: string; actual: string } | undefined {
  const { expectedHandoffTarget: expected, actualHandoffTarget: actual } = carrier
  if (expected === undefined || actual === undefined) return undefined
  return { expected, actual }
}

/**
 * The pair, where the carrier reports one, and nothing where it does not.
 */
export function TargetPair({
  of,
  /** What the two sides are called, so a node's pair reads as the node's. */
  expectedLabel = 'expected target',
  actualLabel = 'actual target'
}: {
  of: HandoffTargetCarrier
  expectedLabel?: string
  actualLabel?: string
}) {
  const pair = handoffTargetPair(of)
  if (!pair) return null
  return (
    <div className="row-compare row-targets">
      <TargetSide label={expectedLabel} member={pair.expected} />
      <TargetSide label={actualLabel} member={pair.actual} />
    </div>
  )
}

/**
 * How a badge should describe an assertion, or nothing where there is none.
 *
 * "Asserts no handoff target" and "asserts a handoff-target state" are two
 * different claims and the literal `null` is what separates them, so the badge
 * reads the member rather than the mere fact of one being present. It reads it
 * through the same accessor the pair does, so a badge can never appear over a
 * pair that is not being shown.
 */
export function describeTargetAssertion(carrier: HandoffTargetCarrier): string | undefined {
  const pair = handoffTargetPair(carrier)
  if (!pair) return undefined
  return pair.expected === NO_HANDOFF_TARGET
    ? 'asserts no handoff target'
    : 'asserts a handoff-target state'
}
