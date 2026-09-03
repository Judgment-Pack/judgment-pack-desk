/**
 * What one member of a pack refers to, and what refers back to it.
 *
 * Every line this produces is a **document fact**: an id this document
 * declares, or an id it names and does not declare. Where an id resolves to
 * nothing the line says exactly that — "no declared outcome carries this id" —
 * and never a word of judgment. `JPS-SEMANTIC-UNRESOLVED-OUTCOME` is the
 * runtime's to issue (`internal/validation/semantic.go`), and a desk that
 * shadowed it with a verdict of its own would be a second opinion nobody asked
 * for and nothing recorded.
 *
 * Both directions, because a reader looking at an outcome wants to know what
 * produces it as much as a reader looking at a rule wants to know what it
 * produces.
 *
 * **`escalation.triggers` produces no line, because it names no id.** It is a
 * closed enum of five reason words — `not-applicable`,
 * `missing-required-evidence`, `unknown`, `conflict`, `no-match`
 * (`$defs/escalation` in the bundled `jps/0.2.0-draft` schema) — and reading
 * one as an evidence-requirement id printed "no declared evidence requirement
 * carries this id" on every conformant pack: a dangling-reference claim about
 * a document that made none. The Escalation block prints the words verbatim
 * and that is the whole of what they are.
 */
import type { Condition, PackDocument } from '../mcp/types'
import { parsePointer, pointer } from './pointers'

/** One line of a References panel. */
export interface Reference {
  /** `outcome`, `evidence`, `sources`, `cited by`, … — the relation's own word. */
  relation: string
  /** The id as the document spells it. */
  id: string
  /** Where the referent is, when this document declares it exactly once. */
  target?: string
  /**
   * Every place this id is declared, when the document declares it more than
   * once.
   *
   * A duplicate id is a document the runtime will refuse, and until it does the
   * desk has to say what it sees. Picking one — which a last-wins map did
   * silently — is the desk inventing a resolution the document does not have,
   * and picking it *by position* means the answer changes if the members are
   * reordered.
   */
  candidates?: string[]
  /** Said instead of a target, where the document declares no such id. */
  unresolved?: string
}

/** Every reference at and around one pointer, both directions. */
export function referencesFor(document: PackDocument | undefined, at: string): Reference[] {
  if (document === undefined || at === '') return []
  const outcomeAt = declaredAt(document.outcomes, 'outcomes')
  const evidenceAt = declaredAt(document.evidenceRequirements, 'evidenceRequirements')
  const sourceAt = declaredAt(document.sources, 'sources')
  const ruleAt = declaredAt(document.rules, 'rules')

  const named = (
    relation: string,
    id: string,
    where: Map<string, string[]>,
    kind: string
  ): Reference => {
    const targets = where.get(id) ?? []
    if (targets.length === 0) {
      return { relation, id, unresolved: `no declared ${kind} carries this id` }
    }
    // Exactly one is the ordinary case and reads as a link. More than one is a
    // document that does not say which, and neither does this.
    if (targets.length === 1) return { relation, id, target: targets[0] }
    return { relation, id, candidates: targets }
  }

  const lines: Reference[] = []

  // A rule.
  const ruleIndex = indexUnder(at, 'rules')
  if (ruleIndex !== undefined) {
    const rule = document.rules?.[ruleIndex]
    if (rule !== undefined) {
      // Guarded, though the type says otherwise: this panel reads documents
      // mid-draft, and `outcome` is exactly the member a draft has not written
      // yet. An unguarded push printed an empty id beside "no declared outcome
      // carries this id" — a sentence about a lookup nobody asked for. Absence
      // is the runtime's diagnostic to issue, at `/rules/N/outcome`.
      if (typeof rule.outcome === 'string') {
        lines.push(named('outcome', rule.outcome, outcomeAt, 'outcome'))
      }
      for (const id of rule.evidenceRequirementRefs ?? []) {
        if (typeof id === 'string') lines.push(named('evidence', id, evidenceAt, 'evidence requirement'))
      }
      for (const id of rule.sourceRefs ?? []) {
        if (typeof id === 'string') lines.push(named('sources', id, sourceAt, 'source'))
      }
      for (const [index, exception] of (document.exceptions ?? []).entries()) {
        if (exception.targetRule === rule.id) {
          lines.push({ relation: 'cited by', id: exception.id, target: pointer(['exceptions', index]) })
        }
      }
    }
  }

  // An exception.
  const exceptionIndex = indexUnder(at, 'exceptions')
  if (exceptionIndex !== undefined) {
    const exception = document.exceptions?.[exceptionIndex]
    if (exception !== undefined) {
      if (exception.targetRule !== undefined) {
        lines.push(named('target rule', exception.targetRule, ruleAt, 'rule'))
      }
      if (exception.outcome !== undefined) {
        lines.push(named('outcome', exception.outcome, outcomeAt, 'outcome'))
      }
      for (const id of exception.sourceRefs ?? []) lines.push(named('sources', id, sourceAt, 'source'))
    }
  }

  // An outcome: what produces it, and whether it is the fallback.
  const outcomeIndex = indexUnder(at, 'outcomes')
  if (outcomeIndex !== undefined) {
    const outcome = document.outcomes?.[outcomeIndex]
    if (outcome !== undefined) {
      for (const [index, rule] of (document.rules ?? []).entries()) {
        if (rule.outcome === outcome.id) {
          lines.push({ relation: 'produced by rule', id: rule.id, target: pointer(['rules', index]) })
        }
      }
      for (const [index, exception] of (document.exceptions ?? []).entries()) {
        if (exception.outcome === outcome.id) {
          lines.push({
            relation: 'produced by exception',
            id: exception.id,
            target: pointer(['exceptions', index])
          })
        }
      }
      if (document.fallbackOutcome === outcome.id) {
        lines.push({ relation: 'fallback outcome', id: outcome.id, target: pointer(['fallbackOutcome']) })
      }
    }
  }

  // An evidence requirement: the rules that reference it, and the
  // `evidence-present` condition nodes that name it — which is a fact the
  // condition tree already holds and nothing else reports.
  const evidenceIndex = indexUnder(at, 'evidenceRequirements')
  if (evidenceIndex !== undefined) {
    const requirement = document.evidenceRequirements?.[evidenceIndex]
    if (requirement !== undefined) {
      for (const [index, rule] of (document.rules ?? []).entries()) {
        if ((rule.evidenceRequirementRefs ?? []).includes(requirement.id)) {
          lines.push({ relation: 'required by rule', id: rule.id, target: pointer(['rules', index]) })
        }
      }
      for (const node of evidencePresentNodes(document)) {
        if (node.id === requirement.id) {
          lines.push({ relation: 'tested by condition', id: requirement.id, target: node.pointer })
        }
      }
    }
  }

  // A source: its citers.
  const sourceIndex = indexUnder(at, 'sources')
  if (sourceIndex !== undefined) {
    const source = document.sources?.[sourceIndex]
    if (source !== undefined) {
      for (const [index, rule] of (document.rules ?? []).entries()) {
        if ((rule.sourceRefs ?? []).includes(source.id)) {
          lines.push({ relation: 'cited by rule', id: rule.id, target: pointer(['rules', index]) })
        }
      }
      for (const [index, exception] of (document.exceptions ?? []).entries()) {
        if ((exception.sourceRefs ?? []).includes(source.id)) {
          lines.push({
            relation: 'cited by exception',
            id: exception.id,
            target: pointer(['exceptions', index])
          })
        }
      }
    }
  }

  // The fallback outcome names an outcome like any other reference does.
  if (at === pointer(['fallbackOutcome']) && document.fallbackOutcome !== undefined) {
    lines.push(named('outcome', document.fallbackOutcome, outcomeAt, 'outcome'))
  }

  return lines
}

/**
 * Where each declared id lives — **every** place, not the last one.
 *
 * A `Map` built by `new Map(entries)` is last-wins, so two outcomes named
 * `approve` left one pointer behind and a rule referring to `approve` linked to
 * `/outcomes/1` as though the document had said so. It had not: it had said the
 * id twice, which is a different fact and one the reader has to see.
 */
function declaredAt(
  entries: readonly { id?: string }[] | undefined,
  member: string
): Map<string, string[]> {
  const where = new Map<string, string[]>()
  ;(entries ?? []).forEach((entry, index) => {
    if (typeof entry?.id !== 'string') return
    const at = pointer([member, index])
    const existing = where.get(entry.id)
    if (existing === undefined) where.set(entry.id, [at])
    else existing.push(at)
  })
  return where
}

/**
 * The array index a pointer sits at or under, for one top-level member.
 *
 * `^\d+$` accepted `01`, which is not an RFC 6901 index — the same leading-zero
 * hole the evaluator had, arriving from the other side. The pointer is parsed
 * properly and the token held to the grammar, so `/rules/01` names nothing here
 * exactly as it names nothing there.
 */
function indexUnder(at: string, member: string): number | undefined {
  const parts = parsePointer(at)
  if (parts === undefined || parts.length < 2 || parts[0] !== member) return undefined
  const token = parts[1]!
  if (!/^(0|[1-9][0-9]*)$/.test(token)) return undefined
  return Number(token)
}

/** Every `evidence-present` node in the document, with its own pointer. */
function evidencePresentNodes(document: PackDocument): { id: string; pointer: string }[] {
  const found: { id: string; pointer: string }[] = []
  const walk = (condition: unknown, path: (string | number)[]) => {
    if (typeof condition !== 'object' || condition === null) return
    const node = condition as Condition
    if (node.op === 'evidence-present' && typeof node.evidenceRequirement === 'string') {
      found.push({ id: node.evidenceRequirement, pointer: pointer(path) })
    }
    if (Array.isArray(node.conditions)) {
      node.conditions.forEach((child, index) => walk(child, [...path, 'conditions', index]))
    }
    if (node.condition !== undefined) walk(node.condition, [...path, 'condition'])
  }
  walk(document.applicability, ['applicability'])
  ;(document.rules ?? []).forEach((rule, index) => walk(rule.when, ['rules', index, 'when']))
  ;(document.exceptions ?? []).forEach((exception, index) =>
    walk(exception.when, ['exceptions', index, 'when'])
  )
  return found
}
