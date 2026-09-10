/**
 * The model: a list the endpoint offers, and a field somebody types into.
 *
 * **The list is asked for on its own, and the field never goes away.** Once a
 * key is stored for the endpoint that is *saved*, this asks that endpoint what
 * models it has and offers them; there is no button, because the answer to
 * "which model" is a list the endpoint already knows and asking a person to
 * press something before they can be shown it is a step with no decision in it.
 * A model absent from the first page of a listing, an endpoint that refuses to
 * list at all, a gateway that routes on a name of its own — each is an ordinary
 * arrangement, so the typed field remains beside the list, and what is saved is
 * whatever is in it.
 *
 * **What is saved is the id, never the label.** The two differ on two of the
 * three protocols — Gemini's `displayName`, Anthropic's `display_name` — and a
 * form that saved what it showed would write a name no endpoint answers to.
 *
 * **The listing asks the endpoint that is *saved*, and only where the draft is
 * that endpoint.** The family and the suffix used to come off the editable
 * draft while the gate came off the saved configuration, so choosing Gemini
 * without saving sent `v1beta/models` to a still-saved OpenAI-compatible
 * endpoint — a request the page composed for one destination and the desk sent
 * to another. Two rules hold that shut: the request is made only while the draft
 * **equals** the saved configuration, and the endpoint it asks about is the
 * saved one, read at the moment of asking.
 *
 * **And the rows are cleared — dropped from state — when the endpoint moves.** A
 * list left standing after a provider or a URL changed is a list of models from
 * somewhere else, offered against a form that no longer says that host; and rows
 * merely *hidden* while the identity differed came back when the URL was changed
 * away and back again, resurrecting an arbitrarily stale listing with no request
 * behind it. Hiding is a rendering decision about state that is still there, and
 * what this needs is for it not to be there.
 *
 * **It is asked only where the key is bound**, too. The relay refuses a request
 * whose credential was entered for another destination, before opening a socket,
 * so a listing made there could only produce that refusal.
 */
import { useEffect, useRef, useState } from 'react'
import type { AssistantEndpointConfig } from '../config/deskConfig'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import type { EndpointDraft } from './endpointDraft'
import { listModels, type ModelRow } from './modelListing'
import { bindModelCall } from './session'

const NOT_BOUND = 'Connect first: this desk asks the endpoint with the key stored for it.'
const NOT_SAVED = 'Save these changes first: this asks the endpoint that is saved.'
const LISTED_NONE = 'The endpoint listed no models.'
const FIRST_PAGE = 'The first page of what the endpoint lists. Anything else is typed in below.'

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

/** One attempt at one endpoint's listing, and everything it can have become. */
interface Listing {
  /** The endpoint identity this attempt is about. */
  of: string
  asking: boolean
  rows?: ModelRow[]
  refusal?: string
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
  const [listing, setListing] = useState<Listing | undefined>(undefined)
  // **Which answer is still the current one.** Every request takes a number and
  // only the latest one may land; a cleanup that cancelled on re-render would
  // cancel the request the render was made to start.
  const generation = useRef(0)

  // **Dropped, not hidden**, the instant the form says a different endpoint.
  // Adjusted during render rather than in an effect, so there is never a frame
  // in which a list from one endpoint is on screen under another's address.
  const here = identityOf(draft)
  if (listing !== undefined && listing.of !== here) setListing(undefined)

  const may = bound && matchesSaved && saved !== null
  const asked = listing?.of === here
  useEffect(() => {
    if (!may || asked || saved === null) return
    // **Read here, at the moment of asking.** What is asked about is the
    // endpoint in the file; a family read later, off a draft somebody kept
    // typing into, would be a suffix composed for one destination and sent to
    // another.
    const target = saved
    const mine = ++generation.current
    setListing({ of: identityOf(target), asking: true })
    // **The desk's capability, bound to the saved family.** The page names a
    // path suffix; the address, this chassis' token and the credential are none
    // of its business. The family is the file's, so a listing cannot talk its
    // way into a query its endpoint does not admit.
    listModels(target.kind, bindModelCall(target.kind)).then(
      (rows) => {
        if (generation.current !== mine) return
        setListing({ of: identityOf(target), asking: false, rows })
      },
      (cause: unknown) => {
        if (generation.current !== mine) return
        setListing({
          of: identityOf(target),
          asking: false,
          refusal: cause instanceof Error ? cause.message : String(cause)
        })
      }
    )
  }, [may, asked, saved])

  const rows = listing?.rows
  const offering = rows !== undefined && rows.length > 0

  return (
    <>
      {offering && (
        <Field label="Model" hint={FIRST_PAGE}>
          {(wiring) => (
            <Select
              {...wiring}
              value={rows.some((row) => row.id === draft.model) ? draft.model : ''}
              // **The id, and never the label.** What the endpoint answers to is
              // the id; the label is for the person reading the list.
              onValueChange={(value) => onChange({ ...draft, model: value })}
              options={rows.map((row) => ({ value: row.id, label: row.label }))}
              placeholder="—"
            />
          )}
        </Field>
      )}

      <Field
        label="Type a model id"
        hint="Exactly as the endpoint spells it."
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

      {listing?.asking === true && <p className="quiet">asking the endpoint…</p>}
      {!bound && <p className="quiet">{NOT_BOUND}</p>}
      {bound && !matchesSaved && <p className="quiet">{NOT_SAVED}</p>}
      {rows !== undefined && rows.length === 0 && <p className="quiet">{LISTED_NONE}</p>}
      {listing?.refusal !== undefined && (
        <p className="quiet">
          <code className="partial-reason">{listing.refusal}</code>
        </p>
      )}
    </>
  )
}
