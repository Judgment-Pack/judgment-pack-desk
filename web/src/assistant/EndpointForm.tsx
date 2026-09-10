/**
 * Admin › Assistant, as one form: the provider, the key, the endpoint, the
 * model, the tools and the tier — and the writes that put them somewhere.
 *
 * **This section carries the desk's two writes**, and each is exactly as wide
 * as its reason. A key must never be pasted into a project file, so it cannot
 * go through the file API — which writes only inside the project — and gets its
 * own endpoint. The `assistant` object of the desk-level file is the other:
 * choosing a model and a tier is something an author does while working, and
 * the alternative is telling them to edit a file in a configuration directory
 * by hand between attempts.
 *
 * **The two writes are one action where a person is doing one thing.** A key is
 * kept bound to the endpoint that is *configured*, so storing one before the
 * endpoint is saved is refused by the chassis — which left a first-time setup
 * as two buttons in an order nobody was told. **Connect** is the pair, in the
 * only order the chassis admits: the endpoint through the desk-level write, and
 * then the key through the key route. Neither route changed. If the write is
 * refused the key is never sent; if the key store is refused the endpoint stays
 * saved, and both refusals are shown where they happened.
 *
 * **A refusal is shown in the decoder's own words.** A 422 answers with the
 * `{key, reason}` list the browser's decoder would produce for the same file,
 * so each problem is rendered against the field its key path names and the rest
 * are rendered whole. Nothing here re-words one: the sentence that repairs the
 * file is the sentence the file's reader wrote.
 *
 * **A 409 is not an error.** The file moved underneath this page and nothing
 * was written; the form keeps every value the author typed, and Reload takes
 * the file on disk so the next Save states a digest that is true. There is no
 * "write anyway" — this is the file that names the endpoint a credential is
 * presented to, and the chassis offers no override for it.
 *
 * **The engine is not a field here.** The slot has one member; a menu with one
 * item is a decision nobody makes. See `ASSISTANT_ENGINES` for the migration a
 * file that names the withdrawn one gets.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  ASSISTANT_TOOLS,
  endpointUrlProblem,
  type AssistantTool,
  type EndpointKind
} from '../config/deskConfig'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { FileRequestError, StaleWrite } from '../files/client'
import { Alert } from '../ui/Alert'
import { AlertPanel } from '../ui/AlertPanel'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import type { AssistantConfigWritten } from './client'
import {
  KIND_OPTIONS,
  TIER_OPTIONS,
  assistantWithoutEndpoint,
  assistantWrite,
  draftFrom,
  seedOf,
  withKind,
  withTool,
  type EndpointDraft
} from './endpointDraft'
import { checkLine, identityOf, useEndpointCheck, type CheckAnswer } from './endpointCheck'
import { KeyField } from './KeyField'
import { keyBinding, type KeyBinding } from './keyBinding'
import { ModelChoice } from './ModelChoice'
import {
  useAssistantKey,
  useRemoveAssistantKey,
  useStoreAssistantKey,
  useUpdateAssistantConfig
} from './queries'

/**
 * The sentence the form refuses to write on, where the page never learned the
 * digest.
 *
 * A read that produced no digest is a page that has not seen the file. Writing
 * with the empty string would be claiming there is none — which is a claim, and
 * a wrong one would replace somebody's file with this form's idea of it.
 */
const NO_DIGEST =
  'This desk has not read its own configuration file, and a write states the bytes it replaces.'

const SAVED = 'Saved. The rest of the file is exactly as it was.'
const CREATED = 'Saved, and the file was created. Nothing else is in it.'
const REMOVED = 'Removed. This desk has no assistant endpoint configured.'
const CONNECTED = 'Saved, and the key is stored on this computer.'

/**
 * The one line a removal confirms, and it is about the key rather than the
 * endpoint.
 *
 * Taking the endpoint away does not take the key away — the key line says so in
 * its own words — but it does mean there is nothing to present it to, and
 * storing another one needs an endpoint to bind it to. Saying that here is what
 * stops a removal reading as "and the key is gone".
 */
const REMOVAL_MEANS =
  'The key stays on this computer, entered for the endpoint you are removing, and goes nowhere.'

/** Beside Connect, where nothing has been typed into the key field. */
const NO_KEY_TYPED = 'Enter the key above to store it with the endpoint.'

/**
 * Where the button is not offered, and why.
 *
 * **A sentence and not a disabled button.** Testing needs an endpoint saved and
 * a key stored for it — the chassis refuses without either, by name — and a
 * control that would refuse is worse than a line saying what is missing. It is
 * one line for both because there is one order: Connect does both, and until it
 * has there is nothing to test.
 */
const NOTHING_TO_TEST = 'Connect first: this asks the saved endpoint with the key stored for it.'

/**
 * And where the address on screen is not the address in the file.
 *
 * The check asks the endpoint that is **saved**, always, and reads its family
 * at the moment of asking — so a request cannot be composed for one destination
 * and sent to another. What this stops is the other half: an answer about the
 * saved endpoint presented under a form showing a different one.
 */
const NOT_SAVED = 'Save these changes first: this asks the endpoint that is saved.'

/**
 * The one sentence a form over a file nobody could read is worth.
 *
 * **It claims no absence.** The fields hold the built-in defaults, which is not
 * "no assistant is configured" — it is this desk not knowing, and a page that
 * said the first would be asserting something about a file it could not open.
 */
const UNAVAILABLE =
  'This desk could not read its own configuration. Nothing below is what it is configured for.'

export function EndpointForm({
  unavailable
}: {
  /**
   * Whether this desk could not read the file these fields are about.
   *
   * The fields then hold the built-in defaults rather than anything anybody
   * configured, so they are shown and not edited: typing into them would be
   * composing a write over a file nobody has seen. Save is refused for the same
   * reason one layer along — there is no digest — and this is what says so
   * before somebody has typed.
   */
  unavailable: boolean
}) {
  const { config, desk } = useEffectiveConfig()
  const client = useQueryClient()
  const write = useUpdateAssistantConfig()
  const key = useAssistantKey()
  const store = useStoreAssistantKey()
  const remove = useRemoveAssistantKey()

  // **Seeded from the file, and re-seeded only while nothing is typed.** The
  // read has usually not answered at first render, and a Save answers with a
  // slot this page then re-reads — both have to reach the fields. An edit in
  // progress must not, which is the same rule that makes Reload after a stale
  // write keep what somebody typed. Adjusted during render rather than in an
  // effect, so the fields are never painted a frame behind the file.
  const seed = seedOf(config.assistant)
  const [seeded, setSeeded] = useState(seed)
  const [draft, setDraft] = useState<EndpointDraft>(() => draftFrom(config.assistant))
  const [dirty, setDirty] = useState(false)
  if (!dirty && seed !== seeded) {
    setSeeded(seed)
    setDraft(draftFrom(config.assistant))
  }
  const [saved, setSaved] = useState<string | undefined>(undefined)
  // Two steps, and the first one only says what the second would do. A
  // destructive action whose primary button is the destructive one is a client
  // with no story about a mis-click.
  const [removing, setRemoving] = useState(false)

  // **The answer to the last write, held until the key read disagrees with it.**
  // The chassis says `keyRebindRequired` at the instant the endpoint moves, and
  // waiting for the key read to be re-fetched would leave the line saying the
  // key is bound for as long as that took. The read is the authority afterwards.
  const [rebindAsked, setRebindAsked] = useState(false)
  // **The field is uncontrolled, and that is the point.** It used to be React
  // state cleared with `setTyped('')` immediately before the request — which
  // reads as synchronous and is not: React batches the update, so `fetch` could
  // begin while both the input and the state still held the key. An
  // uncontrolled input is cleared by assigning to the DOM node, which happens at
  // the instant it is written and not at the next render.
  const keyInput = useRef<HTMLInputElement | null>(null)
  // **Whether the field is empty, and nothing else about it.** A boolean is not
  // a mirror: it says a key was typed, never any of it, and nothing derived from
  // it could be a credential. It exists so that Connect can say what it will
  // actually do rather than offering to store a key nobody entered.
  const [typed, setTyped] = useState(false)
  const [storeProblem, setStoreProblem] = useState<string | undefined>(undefined)
  const [removeProblem, setRemoveProblem] = useState<string | undefined>(undefined)

  // **The one press that asks the endpoint anything**, and the answer it left
  // on screen. It is dropped where the form says a different endpoint, which is
  // the module's own rule and not a rendering decision here.
  const check = useEndpointCheck(draft, config.assistant.endpoint)

  const read = keyBinding(key.data)
  const binding: KeyBinding = rebindAsked && read === 'bound' ? 'rebind' : read
  // **Connect only where the desk has *said* there is no usable key.** A read
  // that has not answered is not "no key" — it is a page that has not been told
  // — so the primary action stays Save until the key route says otherwise. A
  // rule the other way round would flash Connect on every load of a configured
  // desk and name a state nobody established.
  const connecting = binding === 'none' || binding === 'no-endpoint' || binding === 'rebind'

  const edit = (next: EndpointDraft) => {
    setDirty(true)
    setSaved(undefined)
    setDraft(next)
  }

  const digest = desk?.sha256
  // The decoder's own rule, run here so a URL it will refuse is not sent. It is
  // the same function the file's reader uses, so this is not a second opinion
  // about a URL — it is the same one, earlier.
  //
  // **There is no rule for the model here, and there must not be one.** An
  // endpoint with no model chosen is a configuration the schema has, and the
  // whole order this form is in — provider, key, Connect, the list, a pick —
  // depends on the first save going through without one.
  const urlProblem = draft.url.trim() === '' ? undefined : endpointUrlProblem(draft.url.trim())

  const refused = write.error instanceof FileRequestError ? write.error : undefined
  const stale =
    write.error instanceof StaleWrite && write.error.code === 'desk-config-changed'
      ? write.error
      : undefined
  // Every problem the decoder named, and which field each belongs against.
  const problems = refused?.problems ?? []
  const problemFor = (path: string) =>
    problems
      .filter((problem) => problem.key === path)
      .map((problem) => problem.reason)
      .join(' ') || undefined
  const unplaced = problems.filter(
    (problem) => !(PLACED as readonly string[]).includes(problem.key)
  )
  // A refusal that is neither a stale write nor a decoder's list still has to be
  // said: `assistant-key-unbound`, an unusable key store, a body too large.
  const otherRefusal =
    stale === undefined && (refused === undefined || problems.length === 0)
      ? (write.error?.message ?? undefined)
      : undefined

  const commit = (
    assistant: unknown,
    said: (answer: AssistantConfigWritten) => string,
    then?: () => void
  ) => {
    if (digest === undefined) return
    setSaved(undefined)
    setStoreProblem(undefined)
    write.mutate(
      { assistant, ifMatch: digest },
      {
        onSuccess: (answer) => {
          // Re-seeded from the file the chassis read back, not from the draft:
          // the answer is what landed, and a form that showed what it sent would
          // be reporting its own request as an outcome.
          setDirty(false)
          setRemoving(false)
          setSaved(said(answer))
          setRebindAsked(answer.keyRebindRequired)
          then?.()
        }
      }
    )
  }

  /** Take what was typed, clearing the node before the request is made. */
  const takeKey = (): string => {
    const input = keyInput.current
    const value = input?.value ?? ''
    // Cleared on the node, before the request is made. This assignment has
    // taken effect by the next statement; a `setState` would not have.
    if (input) input.value = ''
    setTyped(false)
    return value
  }

  const storeKey = (value: string, onStored?: () => void) => {
    setStoreProblem(undefined)
    store.submit(value, {
      onError: (error) => setStoreProblem(error.message),
      onStored: () => {
        setRebindAsked(false)
        onStored?.()
      }
    })
  }

  const save = () => commit(assistantWrite(draft), (answer) => (answer.created ? CREATED : SAVED))
  const removeEndpoint = () => commit(assistantWithoutEndpoint(draft), () => REMOVED)

  /**
   * The endpoint and its key, in the one order the chassis admits.
   *
   * The key is taken off the node **before** the write, because the write is
   * what clears the form's dirty state and the node must not be read after a
   * re-seed has been through it. It is sent only once the write has landed: a
   * key stored against an endpoint that was refused would be bound to whatever
   * the file said before, which is the opposite of what the person asked for.
   */
  const connect = () => {
    const value = takeKey()
    commit(
      assistantWrite(draft),
      (answer) => (value === '' ? (answer.created ? CREATED : SAVED) : CONNECTED),
      () => {
        // **And the check runs once, on its own**, so the list is there without
        // a second press: this is the moment both of its preconditions are
        // first true, and asking a person to press a button immediately after
        // the one they just pressed is a step with no decision in it.
        if (value !== '') storeKey(value, check.run)
      }
    )
  }

  const busy = write.isPending || store.isPending
  const blocked = digest === undefined || urlProblem !== undefined
  // **Offered once there is an endpoint saved and a key stored for it**, which
  // are the two states the probe and the relay each refuse without. Both are
  // read off what the desk says rather than off the draft: the key state is the
  // chassis' answer, and the endpoint is the one in the file.
  const configured = config.assistant.endpoint
  const connected = configured !== null && (key.data?.present ?? false)
  // **And only while the form says that endpoint.** Compared rather than
  // remembered — a sticky "has been edited" flag would keep the button away
  // after an edit somebody undid — and compared on the two members that decide
  // where a request goes, so ticking a model does not take the button away.
  const here = configured !== null && identityOf(draft) === identityOf(configured)
  const mayTest = connected && here
  const whyNotTest = connected ? NOT_SAVED : NOTHING_TO_TEST

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        if (connecting) connect()
        else save()
      }}
    >
      {unavailable && <p className="quiet">{UNAVAILABLE}</p>}
      {/* Disabled as a whole while a write is in flight — a field edited between
          the request and its answer would be a value the author believes was
          saved and was not — and while the file these fields are about could not
          be read, when they are the built-in defaults rather than anything
          anybody configured. */}
      <fieldset disabled={busy || unavailable}>
        <Field label="Provider" error={problemFor('assistant.endpoint.kind')}>
          {(wiring) => (
            <Select
              {...wiring}
              value={draft.kind}
              onValueChange={(value) => edit(withKind(draft, value as EndpointKind))}
              options={KIND_OPTIONS}
            />
          )}
        </Field>

        <KeyField
          state={key.data}
          answered={key.isSuccess}
          failed={key.error}
          binding={binding}
          field={keyInput}
          onTyped={setTyped}
          onStore={() => storeKey(takeKey())}
          storeProblem={storeProblem}
          onRemove={() => {
            setRemoveProblem(undefined)
            remove.mutate(undefined, {
              onError: (error) => setRemoveProblem(error.message),
              onSettled: () => remove.reset()
            })
          }}
          removeProblem={removeProblem}
        />

        <Field
          label="Endpoint URL"
          hint="Leave the default unless you use a proxy or your own server."
          error={urlProblem ?? problemFor('assistant.endpoint.url')}
        >
          {(wiring) => (
            <Input
              {...wiring}
              value={draft.url}
              spellCheck={false}
              onChange={(event) => edit({ ...draft, url: event.target.value })}
            />
          )}
        </Field>

        {/* **Right after the address, because it is the question a person has
            at that point**: does this work, and what does it offer? One press,
            both answers, and the Models list below fills in from it. */}
        <p className="actions">
          {mayTest && <Button onClick={check.run}>Test connection</Button>}{' '}
          {!mayTest && <span className="quiet">{whyNotTest}</span>}
          {check.answer !== undefined && <CheckReading answer={check.answer} />}
        </p>

        <ModelChoice
          draft={draft}
          // **The rows of the last press, and nothing before one.** Until
          // somebody asks, this desk has been told nothing about what the
          // endpoint offers, and a list drawn from anywhere else would be a
          // claim it did not establish.
          rows={check.answer?.rows}
          onChange={edit}
          problem={problemFor('assistant.endpoint.models')}
          setProblem={problemFor('assistant.endpoint.model')}
        />

        <ToolChoice draft={draft} onChange={edit} problem={problemFor('assistant.endpoint.tools')} />

        <Field
          label="Thinking"
          hint="How much reasoning the model may do before answering."
          error={problemFor('assistant.thinking')}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={draft.thinking}
              onValueChange={(value) =>
                edit({ ...draft, thinking: value as EndpointDraft['thinking'] })
              }
              options={TIER_OPTIONS}
            />
          )}
        </Field>

        <p className="actions">
          <Button variant="primary" type="submit" disabled={blocked || busy}>
            {connecting ? 'Connect' : 'Save'}
          </Button>{' '}
          {/* **The slot's other state, which the schema has and the form did
              not.** `assistant.endpoint` is one nullable field; clearing the
              boxes sends an object the decoder refuses, so without this a desk
              that had configured an endpoint could only get back to None through
              the generic file editor — while this page describes None as one of
              three deployment states. */}
          {config.assistant.endpoint !== null && !removing && (
            <Button
              variant="quiet"
              disabled={digest === undefined || busy}
              onClick={() => setRemoving(true)}
            >
              Remove endpoint
            </Button>
          )}
          {busy && <span className="quiet">writing…</span>}
          {connecting && !typed && !busy && <span className="quiet">{NO_KEY_TYPED}</span>}
          {saved !== undefined && !busy && <span className="quiet">{saved}</span>}
        </p>

        {removing && (
          <p className="quiet">
            {REMOVAL_MEANS}{' '}
            <Button variant="quiet" disabled={busy} onClick={removeEndpoint}>
              Remove it
            </Button>{' '}
            <Button variant="quiet" disabled={busy} onClick={() => setRemoving(false)}>
              Keep it
            </Button>
          </p>
        )}
      </fieldset>

      {digest === undefined && <p className="quiet">{NO_DIGEST}</p>}

      {stale !== undefined && (
        <AlertPanel
          heading="The configuration changed on disk. Nothing was written."
          detailLabel="digests"
          detail={
            <>
              <span>
                this page read{' '}
                <code title={stale.expectedSha256}>sha256 {short(stale.expectedSha256)}</code>
              </span>
              <span>
                on disk now <code title={stale.actualSha256}>sha256 {short(stale.actualSha256)}</code>
              </span>
            </>
          }
          actions={
            <Button
              variant="primary"
              disabled={busy}
              onClick={() => {
                write.reset()
                void client.refetchQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
              }}
            >
              Reload
            </Button>
          }
        >
          <span>
            Everything typed here is still here. Reload reads the file again and keeps these
            fields, so the next Save states a digest that is true.
          </span>
        </AlertPanel>
      )}

      {unplaced.length > 0 && (
        <div role="alert">
          <p>This configuration was refused, and nothing was written.</p>
          {unplaced.map((problem) => (
            <code key={`${problem.key}:${problem.reason}`} className="partial-reason">
              {problem.key === '' ? problem.reason : `${problem.key}: ${problem.reason}`}
            </code>
          ))}
        </div>
      )}

      {otherRefusal !== undefined && (
        <Alert reason={otherRefusal}>Nothing was written.</Alert>
      )}
    </form>
  )
}

/**
 * The key paths this form renders **beside a field**.
 *
 * Everything else the decoder can name is rendered whole, because a problem
 * whose field is not on this form is still a problem with the file this write
 * would have made — and dropping it would leave a refusal with no sentence.
 *
 * `assistant.engine` is still on it. The form has no Engine field and never
 * writes the member, but a file that already carries a value this decoder
 * refuses is refused when this form saves over the rest of it, and a refusal
 * with nowhere to land is a refusal nobody reads.
 */
const PLACED = [
  'assistant.endpoint.url',
  'assistant.endpoint.kind',
  'assistant.endpoint.model',
  'assistant.endpoint.models',
  'assistant.endpoint.tools',
  'assistant.thinking'
] as const

/**
 * The five tools, as five checkboxes.
 *
 * Not a Select and not a multi-select: each is an independent grant, and the
 * empty list is a real choice — an assistant that may call nothing — rather than
 * a state to be prevented. The list is the closed one, so a name outside it
 * cannot be offered here at all.
 */
function ToolChoice({
  draft,
  onChange,
  problem
}: {
  draft: EndpointDraft
  onChange: (next: EndpointDraft) => void
  problem: string | undefined
}) {
  return (
    <fieldset className="tool-choice">
      <legend>Tools the assistant may use</legend>
      {ASSISTANT_TOOLS.map((tool) => (
        <label key={tool} className="checkbox">
          <input
            type="checkbox"
            checked={draft.tools.includes(tool)}
            onChange={(event) => onChange(withTool(draft, tool as AssistantTool, event.target.checked))}
          />{' '}
          <code>{tool}</code>
        </label>
      ))}
      <p className="quiet">All read-only. Untick one to hide it from the assistant.</p>
      {problem !== undefined && <p className="partial-reason">{problem}</p>}
    </fieldset>
  )
}

/**
 * One press of Test connection, reported as it came.
 *
 * `reachable` is the endpoint having answered *successfully*, and a refused
 * credential is therefore not reachable — a page that called a 401 reachable
 * would report a desk that cannot make one call as ready to work. What the
 * line says is `checkLine`'s, so the sentence is one function's and not
 * assembled here; a refusal from elsewhere is quoted rather than narrated.
 */
function CheckReading({ answer }: { answer: CheckAnswer }) {
  const line = checkLine(answer)
  if (line.says === '') return null
  return (
    <span className="quiet">
      {line.says}
      {line.quoted !== undefined && (
        <>
          {' '}
          <code className="partial-reason">{line.quoted}</code>
        </>
      )}
    </span>
  )
}

function short(value: string): string {
  return value ? `${value.slice(0, 12)}…` : '(no file)'
}
