/**
 * **Models**: which of this endpoint's models this desk may run, and which one
 * a run opens on.
 *
 * **A set and a default, because the file has a set and a default.** The single
 * Select that stood here made "which model" a decision taken once, in Admin,
 * for every run afterwards — which is the wrong place for it: one endpoint
 * answers to several models, and which one a piece of work wants is something
 * the person doing the work knows. So Admin decides what is *available* and the
 * run decides what is *used*, and the Default radio is how the set says where a
 * run starts.
 *
 * **A checkbox each, on the tool list's precedent and for the same reason.**
 * Each id is an independent grant, the empty set is a real state — an endpoint
 * with nothing enabled, which the decoder accepts and says so about — and a
 * multi-select would hide both behind one control.
 *
 * **The rows are the endpoint's listing over the file's own set**, in that
 * order, and the file's set is always among them: an id enabled before this
 * listing existed, or one from a page the endpoint no longer returns, is still
 * something this desk is configured for and a list that dropped it would be a
 * form silently disabling a model on the next Save. Before any listing there is
 * the file's set and the Other field, and nothing else — the listing is asked
 * for by **Test connection**, and until somebody presses it this desk has been
 * told nothing about what the endpoint offers.
 *
 * **Other model… is the whole of how an unlisted id is added.** A listing is
 * first-page-only, an endpoint may refuse to list at all, and a gateway may
 * route on a name of its own; none of those may stop an author enabling a model
 * they know the name of. What it is held to is the decoder's own rule — the id
 * rule and the no-duplicates rule, in one function the file's reader calls too —
 * because a copy of either beside this field is how a picker comes to offer
 * something that produces a 422 on the next Save.
 *
 * **What is enabled is the id and never the label.** The two differ on two of
 * the three protocols — Gemini's `displayName`, Anthropic's `display_name` — and
 * a form that saved what it showed would write a name no endpoint answers to.
 */
import { useState } from 'react'
import { modelAddProblem } from '../config/deskConfig'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { withDefaultModel, withModel, type EndpointDraft } from './endpointDraft'
import type { ModelRow } from './modelListing'

/** Under the list, saying what the two controls on each row are for. */
const HOW = 'Tick a model to enable it. Default is the one a run opens on.'

/** Beside the Other field. */
const TYPED = 'Exactly as the endpoint spells it.'

export function ModelChoice({
  draft,
  rows,
  onChange,
  problem,
  setProblem
}: {
  draft: EndpointDraft
  /** What the endpoint said it offers, or undefined where nothing has asked. */
  rows: ModelRow[] | undefined
  onChange: (next: EndpointDraft) => void
  /** The decoder's own sentence about `assistant.endpoint.models`, from a write. */
  problem: string | undefined
  /** The same, about `assistant.endpoint.model`. */
  setProblem: string | undefined
}) {
  const [typed, setTyped] = useState('')
  // **The rule is asked, not restated.** Held against the draft's set, which is
  // what the next Save writes, so a duplicate is refused against the state that
  // would actually carry it.
  const adding = typed.trim() === '' ? undefined : modelAddProblem(typed, draft.models)

  const add = () => {
    if (adding !== undefined || typed.trim() === '') return
    onChange(withModel(draft, typed.trim(), true))
    setTyped('')
  }

  return (
    <fieldset className="model-choice">
      <legend>Models</legend>
      {offered(draft.models, rows).map((row) => {
        const enabled = draft.models.includes(row.id)
        return (
          <p key={row.id} className="model-row">
            <label className="checkbox">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(event) => onChange(withModel(draft, row.id, event.target.checked))}
              />{' '}
              <code>{row.id}</code>
              {row.label !== row.id && <> {row.label}</>}
            </label>{' '}
            {/* **Offered on an enabled row alone.** A default nothing enabled is
                the state the decoder refuses by name, and a radio that could
                reach it would be a control composing a file its own reader
                rejects. */}
            <label className="checkbox">
              <input
                type="radio"
                name="assistant-default-model"
                checked={draft.model === row.id}
                disabled={!enabled}
                onChange={() => onChange(withDefaultModel(draft, row.id))}
              />{' '}
              Default
            </label>
          </p>
        )
      })}

      <p className="quiet">{HOW}</p>

      <Field label="Other model… (type an id)" hint={TYPED} error={adding}>
        {(wiring) => (
          <Input
            {...wiring}
            value={typed}
            spellCheck={false}
            onChange={(event) => setTyped(event.target.value)}
            onKeyDown={(event) => {
              // Enter adds the id rather than submitting the form, which would
              // save an endpoint without the model somebody was in the middle
              // of adding.
              if (event.key !== 'Enter') return
              event.preventDefault()
              add()
            }}
          />
        )}
      </Field>
      <p className="actions">
        <Button disabled={typed.trim() === '' || adding !== undefined} onClick={add}>
          Add
        </Button>
      </p>
      {problem !== undefined && <p className="partial-reason">{problem}</p>}
      {setProblem !== undefined && <p className="partial-reason">{setProblem}</p>}
    </fieldset>
  )
}

/**
 * The rows to show: what the endpoint listed, and every id the file enables
 * that it did not.
 *
 * **The file's set is never dropped.** An id enabled before this listing
 * existed is still something this desk is configured for; a list that showed
 * only what came back would leave a person unticking a box they cannot see, on
 * the next Save.
 *
 * The listing's order first, because that is the endpoint's own; the set's
 * leftovers after it, in the file's order.
 */
export function offered(models: readonly string[], rows: ModelRow[] | undefined): ModelRow[] {
  const listed = rows ?? []
  const seen = new Set(listed.map((row) => row.id))
  return [...listed, ...models.filter((id) => !seen.has(id)).map((id) => ({ id, label: id }))]
}
