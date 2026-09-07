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
 * **List models is offered only where the key is bound.** The relay refuses a
 * request whose credential was entered for another destination, before opening
 * a socket; a button that could only produce that refusal is an affordance
 * that lies about what the page can do.
 */
import { useState } from 'react'
import type { EndpointKind } from '../config/deskConfig'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import type { EndpointDraft } from './endpointDraft'
import { listModels, type ModelRow } from './modelListing'
import { bindModelCall } from './session'

const NOT_BOUND =
  'The endpoint has to be saved and its key stored before this desk can ask it what it has.'
const FIRST_PAGE =
  'The first page of what the endpoint lists, and no more of it. A model that is not here is ' +
  'typed into the field.'

export function ModelField({
  draft,
  bound,
  onChange,
  problem
}: {
  draft: EndpointDraft
  /** Whether the stored key is the key for the endpoint that is **saved**. */
  bound: boolean
  onChange: (next: EndpointDraft) => void
  problem: string | undefined
}) {
  const [rows, setRows] = useState<ModelRow[] | undefined>(undefined)
  const [asking, setAsking] = useState(false)
  const [refusal, setRefusal] = useState<string | undefined>(undefined)

  const ask = () => {
    setAsking(true)
    setRefusal(undefined)
    // **The desk's capability, bound to the configured family.** The page
    // names a path suffix; the address, this chassis' token and the
    // credential are none of its business. The family is the file's, so a
    // listing cannot talk its way into a query its endpoint does not admit.
    listModels(draft.kind as EndpointKind, bindModelCall(draft.kind)).then(
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

  return (
    <>
      <Field
        label="Model"
        hint="The model id this endpoint knows it by, exactly as the endpoint spells it."
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
        <Button onClick={ask} disabled={!bound || asking}>
          List models
        </Button>{' '}
        {asking && <span className="quiet">asking the endpoint…</span>}
        {!bound && <span className="quiet">{NOT_BOUND}</span>}
        {refusal !== undefined && !asking && (
          <span className="quiet">
            <code className="partial-reason">{refusal}</code>
          </span>
        )}
      </p>

      {rows !== undefined && (
        <Field
          label="Models this endpoint listed"
          hint={rows.length === 0 ? 'The endpoint listed none.' : FIRST_PAGE}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={rows.some((row) => row.id === draft.model) ? draft.model : ''}
              // **The id, and never the label.** What the endpoint answers to
              // is the id; the label is for the person reading the list.
              onValueChange={(value) => onChange({ ...draft, model: value })}
              options={rows.map((row) => ({ value: row.id, label: row.label }))}
              placeholder="—"
            />
          )}
        </Field>
      )}
    </>
  )
}
