/**
 * The model: a field somebody types into, and a list the endpoint offers.
 *
 * **The field is the primary control and the list is an aid to it.** A model
 * absent from the first page of a listing, an endpoint that refuses to list at
 * all, a gateway that routes on a name of its own — each is an ordinary
 * arrangement, and a picker that was the only way to choose would make every
 * one of them unconfigurable. So the field never goes away, the list fills it,
 * and what is saved is what is in the field.
 *
 * **What is saved is the id, never the label.** The two differ on two of the
 * three protocols — Gemini's `displayName`, Anthropic's `display_name` — and a
 * form that saved what it showed would write a name no endpoint answers to.
 *
 * **List models asks the endpoint that is *saved*, and is offered only where
 * the draft is that endpoint.** The family and the suffix used to come off the
 * editable draft while the gate came off the saved configuration, so choosing
 * Gemini without saving sent `v1beta/models` to a still-saved
 * OpenAI-compatible endpoint — a request the page composed for one destination
 * and the desk sent to another. Two rules hold that shut now: the button is
 * enabled only while the draft **equals** the saved configuration, and the
 * endpoint it asks about is the saved one, captured at the click.
 *
 * **And the rows are cleared — dropped from state — when the endpoint moves.**
 * A picker left standing after a kind or a URL changed is a list of models from
 * somewhere else, offered against a form that no longer says that host; and
 * rows merely *hidden* while the identity differed came back when the URL was
 * changed away and back again, resurrecting an arbitrarily stale listing with
 * no request behind it. Hiding is a rendering decision about state that is
 * still there, and what this needs is for it not to be there.
 *
 * **It is offered only where the key is bound**, too. The relay refuses a
 * request whose credential was entered for another destination, before opening
 * a socket; a button that could only produce that refusal is an affordance
 * that lies about what the page can do.
 */
import { useState } from 'react'
import type { AssistantEndpointConfig } from '../config/deskConfig'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import type { EndpointDraft } from './endpointDraft'
import { listModels, type ModelRow } from './modelListing'
import { bindModelCall } from './session'

const NOT_BOUND = 'Save the endpoint and store its key before this desk can ask what it has.'
const NOT_SAVED = 'Save these changes first: this asks the endpoint that is saved.'
const FIRST_PAGE = 'The first page of what the endpoint lists. Anything else is typed in.'

/**
 * Which endpoint a set of rows is about.
 *
 * The two members that decide where a listing goes and what it means: change
 * either and the rows on screen are a list from somewhere else. The model is
 * deliberately not in it — picking one from the list must not empty the list.
 */
function identityOf(endpoint: { kind: string; url: string }): string {
  return `${endpoint.kind}\n${endpoint.url}`
}

export function ModelField({
  draft,
  saved,
  bound,
  matchesSaved,
  onChange,
  problem
}: {
  draft: EndpointDraft
  /** The endpoint in the file. The listing asks this one and no other. */
  saved: AssistantEndpointConfig | null
  /** Whether the stored key is the key for the endpoint that is **saved**. */
  bound: boolean
  /** Whether the draft on screen is that same endpoint, member for member. */
  matchesSaved: boolean
  onChange: (next: EndpointDraft) => void
  problem: string | undefined
}) {
  const [rows, setRows] = useState<ModelRow[] | undefined>(undefined)
  const [askedFor, setAskedFor] = useState<string | undefined>(undefined)
  const [asking, setAsking] = useState(false)
  const [refusal, setRefusal] = useState<string | undefined>(undefined)

  const ask = () => {
    // **Captured here, at the click.** What is asked about is the endpoint in
    // the file; a family read later, off a draft somebody kept typing into,
    // would be a suffix composed for one destination and sent to another.
    const target = saved
    if (target === null) return
    setAsking(true)
    setRefusal(undefined)
    setAskedFor(identityOf(target))
    // **The desk's capability, bound to the saved family.** The page names a
    // path suffix; the address, this chassis' token and the credential are
    // none of its business. The family is the file's, so a listing cannot talk
    // its way into a query its endpoint does not admit.
    listModels(target.kind, bindModelCall(target.kind)).then(
      (listed) => {
        setRows(listed)
        setAsking(false)
      },
      (cause: unknown) => {
        setRefusal(cause instanceof Error ? cause.message : String(cause))
        setRows(undefined)
        setAsking(false)
      }
    )
  }

  // **Dropped, not hidden**, the instant the form says a different endpoint.
  // Adjusted during render rather than in an effect, so there is never a frame
  // in which a list from one endpoint is on screen under another's address.
  const here = identityOf(draft)
  if (rows !== undefined && askedFor !== here) {
    setRows(undefined)
    setAskedFor(undefined)
    setRefusal(undefined)
  }
  const showing = rows !== undefined

  return (
    <>
      <Field
        label="Model"
        hint="The model id, exactly as the endpoint spells it."
        error={problem}
      >
        {(wiring) => (
          <Input
            {...wiring}
            value={draft.model}
            spellCheck={false}
            onChange={(event) => onChange({ ...draft, model: event.target.value })}
          />
        )}
      </Field>

      <p className="actions">
        <Button onClick={ask} disabled={!bound || !matchesSaved || asking}>
          List models
        </Button>{' '}
        {asking && <span className="quiet">asking the endpoint…</span>}
        {!bound && <span className="quiet">{NOT_BOUND}</span>}
        {bound && !matchesSaved && <span className="quiet">{NOT_SAVED}</span>}
        {refusal !== undefined && !asking && (
          <span className="quiet">
            <code className="partial-reason">{refusal}</code>
          </span>
        )}
      </p>

      {showing && (
        <Field
          label="Models this endpoint listed"
          hint={rows!.length === 0 ? 'The endpoint listed none.' : FIRST_PAGE}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={rows!.some((row) => row.id === draft.model) ? draft.model : ''}
              // **The id, and never the label.** What the endpoint answers to
              // is the id; the label is for the person reading the list.
              onValueChange={(value) => onChange({ ...draft, model: value })}
              options={rows!.map((row) => ({ value: row.id, label: row.label }))}
              placeholder="—"
            />
          )}
        </Field>
      )}
    </>
  )
}
