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
 * requested no handoff — and `unavailable` is the absence of one, reachable
 * where a run was refused under an expected composite. Collapsing them would
 * turn a refused run into a policy that hands off nowhere.
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
 * The pair, where the row or comparison asserts one, and nothing where it does
 * not.
 *
 * Presence is decided on the *expected* member alone, because that is the one
 * the assertion is: a row that asserts nothing carries neither, and a row that
 * asserts "no target at all" carries both, each as the literal `null`.
 */
export function TargetPair({
  expected,
  actual,
  /** What the two sides are called, so a node's pair reads as the node's. */
  expectedLabel = 'expected target',
  actualLabel = 'actual target'
}: {
  expected: string | undefined
  actual: string | undefined
  expectedLabel?: string
  actualLabel?: string
}) {
  if (expected === undefined) return null
  return (
    <div className="row-compare row-targets">
      <TargetSide label={expectedLabel} member={expected} />
      <TargetSide label={actualLabel} member={actual} />
    </div>
  )
}

/**
 * How a badge should describe an assertion, or nothing where there is none.
 *
 * "Asserts no handoff target" and "asserts a handoff-target state" are two
 * different claims and the literal `null` is what separates them, so the badge
 * reads the member rather than the mere fact of one being present.
 */
export function describeTargetAssertion(expected: string | undefined): string | undefined {
  if (expected === undefined) return undefined
  return expected === NO_HANDOFF_TARGET
    ? 'asserts no handoff target'
    : 'asserts a handoff-target state'
}
