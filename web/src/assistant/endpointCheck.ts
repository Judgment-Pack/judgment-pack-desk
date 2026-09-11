/**
 * **Test connection**: one press, one question — *can this desk reach that
 * endpoint, and what does it offer?*
 *
 * **The two halves are one action because they are one question.** The
 * reachability probe says whether the credential and the address work; the
 * model listing says what there is to enable. Asking them separately meant two
 * controls for one moment in somebody's setup, and a list that arrived on its
 * own the instant a key was stored — before anybody had said they wanted this
 * endpoint asked anything. The check runs only when its button is pressed;
 * saving a key leaves the connection untested.
 *
 * **Neither route changed, and neither is built here.** The probe carries no
 * destination — the chassis reads the file — and the listing names a path
 * suffix that `bindModelCall` turns into an address. This module holds a
 * generation counter and some state; there is no `fetch` in it.
 *
 * **Anthropic with nothing enabled is refused before anything is sent.** That
 * protocol's probe is a *generation* call, so a desk with no model would put a
 * request naming the empty string on the wire and read whatever came back as a
 * verdict about the endpoint. It is not one. The button says so instead, and
 * the reason it says so here rather than at the chassis is that the repair is
 * here: enable a model, then test.
 *
 * **And the answer is dropped — not hidden — the moment the form says a
 * different endpoint.** An answer left standing after a provider or a URL
 * changed is a reading of somewhere else, offered against a form that no longer
 * says that host; rows merely hidden while the identity differed came back when
 * the URL was typed away and back again, resurrecting an arbitrarily stale
 * listing with no request behind it.
 */
import { useRef, useState } from 'react'
import type { AssistantEndpointConfig } from '../config/deskConfig'
import { DIAGNOSTIC_SAYS, probeAssistantEndpoint, type ProbeResult } from './client'
import type { EndpointDraft } from './endpointDraft'
import { listModels, type ModelRow } from './modelListing'
import { bindModelCall } from './session'

/**
 * What the button says where the protocol's probe would have to name a model
 * and there is none to name.
 *
 * The page's own sentence, about the page's own refusal to send: no decoder
 * says this, and nothing here claims one did.
 */
export const CHOOSE_A_MODEL = 'Choose a model to test this provider.'

/**
 * Which endpoint an answer is about.
 *
 * The two members that decide where a request goes and what its answer means:
 * change either and what is on screen is a reading of somewhere else. The model
 * is deliberately not in it — enabling one must not throw the answer away.
 */
export function identityOf(endpoint: { kind: string; url: string }): string {
  return `${endpoint.kind}\n${endpoint.url.trim()}`
}

/** One press, and everything it can have become. */
export interface CheckAnswer {
  /** The endpoint identity this answer is about. */
  of: string
  asking: boolean
  /** The page's own refusal to send, where it refused. No request was made. */
  refusedToAsk?: string
  probe?: ProbeResult
  /** The probe route's refusal, in the chassis' own words. */
  probeRefusal?: string
  rows?: ModelRow[]
  /** The listing's refusal, in the probe's closed vocabulary. */
  listingRefusal?: string
}

export interface EndpointCheck {
  /** The answer about the endpoint the form says, or undefined. */
  answer: CheckAnswer | undefined
  /** Ask: the probe and the listing, in one press. */
  run: () => void
  /** Discard readings after credentials change, including late responses. */
  reset: () => void
}

/**
 * The check, held against the endpoint the form is showing.
 *
 * `saved` is the endpoint in the *file* and is what both halves ask about —
 * the probe by not naming a destination at all, the listing by taking its
 * family here, at the moment of asking. `draft` decides only whether an answer
 * already on screen is still about the endpoint on screen.
 */
export function useEndpointCheck(draft: EndpointDraft, saved: AssistantEndpointConfig | null): EndpointCheck {
  const [answer, setAnswer] = useState<CheckAnswer | undefined>(undefined)
  // **Which answer is still the current one.** Every press takes a number and
  // only the latest may land; a cleanup that cancelled on re-render would
  // cancel the request the render was made to start.
  const generation = useRef(0)
  const inFlight = useRef<number | undefined>(undefined)
  const reset = () => {
    generation.current += 1
    inFlight.current = undefined
    setAnswer(undefined)
  }

  // **Dropped, not hidden**, the instant the form says a different endpoint.
  // Adjusted during render rather than in an effect, so there is never a frame
  // in which a reading of one endpoint is on screen under another's address.
  const here = identityOf(draft)
  if (answer !== undefined && answer.of !== here) reset()

  const run = () => {
    if (saved === null || inFlight.current !== undefined) return
    // **Read here, at the moment of asking.** What is asked about is the
    // endpoint in the file; a family read later, off a draft somebody kept
    // typing into, would be a suffix composed for one destination and sent to
    // another.
    const target = saved
    const of = identityOf(target)
    if (target.kind === 'anthropic' && target.model === null) {
      setAnswer({ of, asking: false, refusedToAsk: CHOOSE_A_MODEL })
      return
    }
    const mine = ++generation.current
    inFlight.current = mine
    setAnswer({ of, asking: true })
    const landed = (change: (previous: CheckAnswer) => CheckAnswer) => {
      if (generation.current !== mine) return
      const asking = !settled()
      setAnswer((previous) =>
        previous === undefined || previous.of !== of ? previous : { ...change(previous), asking }
      )
    }
    // **Both, and neither waits for the other.** They are one question to a
    // reader and two requests on the wire, and a listing held back until a
    // probe had answered would be slower for no reason a reader could name.
    let outstanding = 2
    const settled = (): boolean => {
      const done = (outstanding -= 1) === 0
      if (done && inFlight.current === mine) inFlight.current = undefined
      return done
    }
    void probeAssistantEndpoint().then(
      (result) => landed((previous) => ({ ...previous, probe: result })),
      (cause: unknown) =>
        landed((previous) => ({ ...previous, probeRefusal: said(cause) }))
    )
    // **The desk's capability, bound to the saved family.** The page names a
    // path suffix; the address, this chassis' token and the credential are none
    // of its business. The family is the file's, so a listing cannot talk its
    // way into a query its endpoint does not admit.
    void listModels(target.kind, bindModelCall(target.kind)).then(
      (rows) => landed((previous) => ({ ...previous, rows })),
      (cause: unknown) =>
        landed((previous) => ({ ...previous, listingRefusal: said(cause) }))
    )
  }

  return { answer: answer?.of === here ? answer : undefined, run, reset }
}

function said(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

/**
 * What the button's line says, split into the desk's own words and whatever it
 * is quoting.
 *
 * **The two are separated because one of them is not this desk's prose.** A
 * refusal from the chassis or from the listing is quoted material — it belongs
 * in a `<code>`, where the narration sweep does not count it and a reader can
 * see that the sentence is somebody else's. What is left is short, plain, and
 * this desk's to answer for.
 */
export interface CheckLine {
  says: string
  /** A refusal from elsewhere, rendered as quoted material. */
  quoted?: string
}

export const ASKING = 'Checking credentials and loading models…'
export const TEST_REFUSED = 'The test was refused:'

/**
 * One answer, read into a line.
 *
 * The order is the order a reader needs: what this desk would not do, then what
 * it is doing, then what came back. A probe that did not answer is reported
 * before a listing that did, because a desk that cannot reach the endpoint has
 * no news about its models worth reading.
 */
export function checkLine(answer: CheckAnswer): CheckLine {
  if (answer.refusedToAsk !== undefined) return { says: answer.refusedToAsk }
  if (answer.asking) return { says: ASKING }
  if (answer.probeRefusal !== undefined) {
    return { says: TEST_REFUSED, quoted: answer.probeRefusal }
  }
  const probe = answer.probe
  if (probe === undefined) return { says: '' }
  if (!probe.reachable) {
    const answered = probe.status === 0 ? 'no answer arrived' : `answered ${probe.status}`
    const says =
      probe.diagnostic === ''
        ? `Not connected · ${answered}`
        : `Not connected · ${answered} · ${DIAGNOSTIC_SAYS[probe.diagnostic] ?? probe.diagnostic}`
    return { says }
  }
  if (answer.listingRefusal !== undefined) {
    return { says: 'Connected · the models could not be listed:', quoted: answer.listingRefusal }
  }
  const rows = answer.rows
  if (rows === undefined) return { says: 'Connected' }
  if (rows.length === 0) return { says: 'Connected · the endpoint listed no models' }
  return { says: `Connected · ${rows.length} model${rows.length === 1 ? '' : 's'} available` }
}
