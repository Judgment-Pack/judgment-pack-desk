/**
 * Admin › Assistant, as a form: the endpoint, the tools, the model, the engine
 * and the tier — and the one conditional commit that writes them.
 *
 * **This is the first configuration surface on this desk that writes a
 * configuration value**, and every bound on it is the chassis'. The request
 * names no file; it carries `ifMatch`, the digest of the bytes this page last
 * read; the chassis composes the file, decodes it under the contract this page
 * decodes by, and refuses the whole write where anything is wrong. So the only
 * thing this component may get wrong is *what it asks for*, and the two ways it
 * could are held here: the object is composed by naming members
 * (`assistantWrite`), and the digest is the one the read carried rather than
 * one this page assumed.
 *
 * **A refusal is shown in the decoder's own words.** A 422 answers with the
 * `{key, reason}` list the browser's decoder would produce for the same file,
 * so each problem is rendered against the field its key path names and the
 * rest are rendered whole. Nothing here re-words one: the sentence that
 * repairs the file is the sentence the file's reader wrote.
 *
 * **A 409 is not an error.** The file moved underneath this page and nothing
 * was written; the form keeps every value the author typed, and Reload takes
 * the file on disk so the next Save states a digest that is true. There is no
 * "write anyway" — this is the file that names the endpoint a credential is
 * presented to, and the chassis offers no override for it.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
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
  ENGINE_OPTIONS,
  ENGINE_SAYS,
  KIND_OPTIONS,
  TIER_OPTIONS,
  assistantWithoutEndpoint,
  assistantWrite,
  draftFrom,
  seedOf,
  tierProvenance,
  tierSays,
  withKind,
  withTool,
  type EndpointDraft
} from './endpointDraft'
import { ModelField } from './ModelField'
import { useUpdateAssistantConfig } from './queries'

/**
 * The sentence the form refuses to write on, where the page never learned the
 * digest.
 *
 * A read that produced no digest is a page that has not seen the file. Writing
 * with the empty string would be claiming there is none — which is a claim,
 * and a wrong one would replace somebody's file with this form's idea of it.
 */
const NO_DIGEST =
  'This desk has not been able to read its own configuration file, so nothing can be written ' +
  'to it: a write states the bytes it is replacing, and this page has not seen them.'

const SAVED = 'Saved. The rest of the file is exactly as it was.'
const CREATED = 'Saved, and the file was created. Nothing else is in it.'
const REMOVED = 'Removed. This desk has no assistant endpoint configured.'

/**
 * The one line a removal confirms, and it is about the key rather than the
 * endpoint.
 *
 * Taking the endpoint away does not take the key away — the two are separate,
 * which the section says in its own words — but it does mean there is nothing
 * to present it to, and storing another one needs an endpoint to bind it to.
 * Saying that here is what stops a removal reading as "and the key is gone".
 */
const REMOVAL_MEANS =
  'The key stays on this machine, still entered for the endpoint you are removing, and this ' +
  'desk will not present it anywhere. Configuring an endpoint again is what makes it usable ' +
  'again, and a key entered for a different one has to be entered again.'

export function EndpointForm({
  bound,
  unavailable,
  onWritten
}: {
  /**
   * Whether the stored key is the key for the endpoint that is **saved**.
   *
   * The saved one and not the draft: a key is bound to what is in the file,
   * and a host typed but not written has changed nothing about where the
   * credential may go. It gates List models and nothing else.
   */
  bound: boolean
  /**
   * Whether this desk could not read the file these fields are about.
   *
   * The fields then hold the built-in defaults rather than anything anybody
   * configured, so they are shown and not edited: typing into them would be
   * composing a write over a file nobody has seen. Save is refused for the
   * same reason one layer along — there is no digest — and this is what says
   * so before somebody has typed.
   */
  unavailable: boolean
  /** Called with every answer to a write that landed. */
  onWritten: (answer: AssistantConfigWritten) => void
}) {
  const { config, desk } = useEffectiveConfig()
  const client = useQueryClient()
  const write = useUpdateAssistantConfig()

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
  // destructive action whose primary button is the destructive one is a
  // client with no story about a mis-click.
  const [removing, setRemoving] = useState(false)

  const edit = (next: EndpointDraft) => {
    setDirty(true)
    setSaved(undefined)
    setDraft(next)
  }

  const digest = desk?.sha256
  // The decoder's own rule, run here so a URL it will refuse is not sent. It
  // is the same function the file's reader uses, so this is not a second
  // opinion about a URL — it is the same one, earlier.
  const urlProblem = draft.url.trim() === '' ? undefined : endpointUrlProblem(draft.url.trim())

  const refused = write.error instanceof FileRequestError ? write.error : undefined
  const stale =
    write.error instanceof StaleWrite && write.error.code === 'desk-config-changed'
      ? write.error
      : undefined
  // Every problem the decoder named, and which field each belongs against.
  const problems = refused?.problems ?? []
  const problemFor = (key: string) =>
    problems
      .filter((problem) => problem.key === key)
      .map((problem) => problem.reason)
      .join(' ') || undefined
  const unplaced = problems.filter(
    (problem) => !(PLACED as readonly string[]).includes(problem.key)
  )
  // A refusal that is neither a stale write nor a decoder's list still has to
  // be said: `assistant-key-unbound`, an unusable key store, a body too large.
  const otherRefusal =
    stale === undefined && (refused === undefined || problems.length === 0)
      ? (write.error?.message ?? undefined)
      : undefined

  const commit = (assistant: unknown, said: (answer: AssistantConfigWritten) => string) => {
    if (digest === undefined) return
    setSaved(undefined)
    write.mutate(
      { assistant, ifMatch: digest },
      {
        onSuccess: (answer) => {
          // Re-seeded from the file the chassis read back, not from the draft:
          // the answer is what landed, and a form that showed what it sent
          // would be reporting its own request as an outcome.
          setDirty(false)
          setRemoving(false)
          setSaved(said(answer))
          onWritten(answer)
        }
      }
    )
  }

  const save = () => commit(assistantWrite(draft), (answer) => (answer.created ? CREATED : SAVED))
  const removeEndpoint = () => commit(assistantWithoutEndpoint(draft), () => REMOVED)

  const busy = write.isPending
  const blocked = digest === undefined || urlProblem !== undefined

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        save()
      }}
    >
      {/* Disabled as a whole while a write is in flight — a field edited
          between the request and its answer would be a value the author
          believes was saved and was not — and while the file these fields are
          about could not be read, when they are the built-in defaults rather
          than anything anybody configured. */}
      <fieldset disabled={busy || unavailable}>
        <Field
          label="Wire protocol"
          hint="It says how a request is shaped — which header carries the key and which path the call goes on — and nothing about who is at the other end."
          error={problemFor('assistant.endpoint.kind')}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={draft.kind}
              onValueChange={(value) => edit(withKind(draft, value as EndpointKind))}
              options={KIND_OPTIONS}
            />
          )}
        </Field>

        <Field
          label="Endpoint"
          hint="The base this protocol documents. Choosing a protocol offers the address its own reference names; type over it for a proxy or an endpoint you run."
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

        <ToolChoice draft={draft} onChange={edit} problem={problemFor('assistant.endpoint.tools')} />

        <ModelField
          draft={draft}
          saved={config.assistant.endpoint}
          bound={bound}
          // **Compared rather than remembered.** A sticky "has been edited"
          // flag would keep the listing disabled after an edit somebody undid;
          // what the gate is actually about is whether the form on screen *is*
          // the endpoint the listing would ask.
          matchesSaved={seed === JSON.stringify(draft)}
          onChange={edit}
          problem={problemFor('assistant.endpoint.model')}
        />

        <Field
          label="Engine"
          hint={ENGINE_SAYS[draft.engine].join(' ')}
          error={problemFor('assistant.engine')}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={draft.engine}
              onValueChange={(value) =>
                edit({ ...draft, engine: value as EndpointDraft['engine'] })
              }
              options={ENGINE_OPTIONS}
            />
          )}
        </Field>

        <Field
          label="Thinking"
          hint={
            <>
              <span>
                On this protocol <code>{draft.thinking}</code> sends{' '}
                <code>{tierSays(draft.kind, draft.thinking)}</code>.
              </span>
              {tierProvenance(draft.kind) !== undefined && (
                <span> {tierProvenance(draft.kind)}</span>
              )}
            </>
          }
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
            Save
          </Button>{' '}
          {/* **The slot's other state, which the schema has and the form did
              not.** `assistant.endpoint` is one nullable field; clearing the
              boxes sends an object the decoder refuses, so without this a desk
              that had configured an endpoint could only get back to None
              through the generic file editor — while this page describes None
              as one of three deployment states. */}
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
            Something else wrote to it since this page read it. Everything you have typed is
            still here: Reload reads the file again and keeps these fields, so Save can state a
            digest that is true. There is no overwrite here — this is the file that names where
            a key is presented.
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
 */
const PLACED = [
  'assistant.endpoint.url',
  'assistant.endpoint.kind',
  'assistant.endpoint.model',
  'assistant.endpoint.tools',
  'assistant.engine',
  'assistant.thinking'
] as const

/**
 * The five tools, as five checkboxes.
 *
 * Not a Select and not a multi-select: each is an independent grant, and the
 * empty list is a real choice — an assistant that may call nothing — rather
 * than a state to be prevented. The list is the closed one, so a name outside
 * it cannot be offered here at all.
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
      <legend>Tools it may call</legend>
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
      <p className="quiet">
        Every one of them is a read: four questions put to the runtime and a rehearsal, which
        consults no reviewed set and decides no outcome. Turning them all off is a real choice
        and means an assistant that may call nothing.
      </p>
      {problem !== undefined && <p className="partial-reason">{problem}</p>}
    </fieldset>
  )
}

function short(value: string): string {
  return value ? `${value.slice(0, 12)}…` : '(no file)'
}
