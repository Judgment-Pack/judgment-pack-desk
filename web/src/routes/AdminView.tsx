/**
 * Admin: six read-only sections about this machine's desk.
 *
 * **Nothing on this page writes configuration**, and that is the decision
 * rather than the state of the work. A real editor needs a `PUT` to a file
 * outside the chassis' pinned `os.Root` — a second containment argument, its
 * own token and origin tests, and a ruling on whether the session token alone
 * may write the identity slot. So Admin renders the effective value, the badge
 * saying where it came from, the path it would come from, and the exact JSON
 * to paste.
 *
 * **Every field is text.** There is no disabled radio group and there are no
 * disabled inputs, deliberately departing from the artboard: a disabled
 * control that will never enable is an affordance that lies about what the
 * page can do. The only control that changes **persisted desk-layout state**
 * is Panes' reset, which clears one `localStorage` key. That is the exact
 * claim and not a rounding of it: the Copy buttons beside each paste block
 * change the clipboard, and their own transient "copied" state with it —
 * calling them state-free was a sentence this file's own `useState` refuted.
 *
 * `runtime.jpackBin` and `project.dir` are **not in the schema at all** —
 * `relay.go` runs the configured binary, so a config-supplied path would be a
 * local-code-execution surface. Those two sections display what the process
 * was started with, and nothing here can change them.
 */
import { useState } from 'react'
import { Json, Section } from '../components/primitives'
import { useHashTarget } from '../shell/useHashTarget'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { DESK_FALLBACK_NAME, PANE_BOUNDS } from '../config/deskConfig'
import { useMcp } from '../mcp/McpProvider'
import { usePacks } from '../mcp/queries'
import { useRenderedPanes, type MeasuredBox } from '../shell/measured'
import { useShellState } from '../shell/paneState'
import { IconCopy } from '../shell/icons'
import type { ResetOutcome } from '../shell/paneState'
import { ADMIN_DISCLAIMER, ADMIN_SECTIONS } from './adminSections'

const DESK_FILE_NOTE =
  'The desk-level desk.json — the only place an identity provider may be configured — is not ' +
  'read yet. Whether the page learns it through a new read-only GET /api/desk-config, or the ' +
  'chassis tells the page at connect time, is the spec’s open question 2 and is unanswered.'

export function AdminView() {
  const { config, sources, problems, path, note, readFailure } = useEffectiveConfig()
  const { server, known } = useMcp()
  const { data } = usePacks()
  const shell = useShellState()
  // Re-measured when a pane is toggled: a pane arriving or leaving is not a
  // resize of anything already observed.
  const rendered = useRenderedPanes(
    `${shell.left.mode}|${shell.inspector.open}|${shell.console.open}`
  )
  const [reset, setReset] = useState<ResetOutcome | undefined>(undefined)
  // The rail's and the user menu's section links carry a hash. Nothing in the
  // router scrolls to one, and the document is not the scroll container here —
  // `.desk-main` is — so without this they changed the URL and moved nothing.
  useHashTarget()

  return (
    <article className="detail">
      <header className="detail-head">
        <h1>Admin</h1>
        <p className="quiet">{ADMIN_DISCLAIMER}</p>
      </header>

      {problems.length > 0 && (
        <p className="note note-warn" role="status">
          <strong>The project configuration was refused, and the desk is on its defaults.</strong>{' '}
          Every problem is named below; one problem refuses the whole file, because a file that
          applied three of its four settings would look honoured while a typo did nothing.
          <br />
          {problems.map((problem) => (
            <code key={`${problem.key}:${problem.reason}`} className="partial-reason">
              {problem.key === '' ? problem.reason : `${problem.key}: ${problem.reason}`}
            </code>
          ))}
        </p>
      )}

      {problems.length === 0 && readFailure !== undefined && (
        <p className="note note-warn" role="status">
          <strong>The project configuration could not be read, and the desk is on its
          defaults.</strong>{' '}
          This is not the same as having no file: the desk asked for <code>{path}</code> and the
          read did not succeed, so whatever it says has not been applied — and nothing here
          establishes that the file is absent either.{' '}
          {/* Attribution off the carried provenance, never off "is there a
              status?": a 200 whose body is not the envelope this API promises
              is an answer whose *sentence* is the desk's own, and inferring
              it had this page say the request never got an answer. */}
          {!readFailure.responseReceived ? (
            <>
              <strong>The request never got an answer</strong>, so the reason below is the
              browser&apos;s own. It says the read failed; it says nothing about what is on
              disk.
            </>
          ) : readFailure.source === 'chassis' ? (
            <>
              The chassis answered <code>{readFailure.status}</code>, and the reason below is
              its own, verbatim.
            </>
          ) : (
            <>
              The chassis answered <code>{readFailure.status}</code>, but the reason below is
              this desk&apos;s sentence about that answer rather than the chassis&apos; own —
              it sent nothing this desk could quote.
            </>
          )}
          <br />
          <code className="partial-reason">{readFailure.reason}</code>
        </p>
      )}

      <h2 id={ADMIN_SECTIONS[0]!.id} className="section-title">
        {ADMIN_SECTIONS[0]!.title}
      </h2>
      <p>
        Name: <strong>{config.organization.name ?? `${DESK_FALLBACK_NAME} (no name configured)`}</strong>
        <br />
        Mark: {config.organization.mark ? 'configured in the project file' : 'none — a monogram'}
        <br />
        <SourceBadge source={sources.organization} path={path} />
      </p>
      <p className="quiet">
        The organization name is local configuration. It is never taken from a token claim, never
        sent to the runtime, and never presented as attested. Absent, the header reads{' '}
        <code>{DESK_FALLBACK_NAME}</code> rather than an invented company.
      </p>
      <PasteBlock
        label="Add to jpack-desk.json"
        json={{ deskConfigVersion: 1, organization: { name: 'Acme Co.', mark: null } }}
      />

      <h2 id={ADMIN_SECTIONS[1]!.id} className="section-title">
        {ADMIN_SECTIONS[1]!.title}
      </h2>
      <p>
        Provider:{' '}
        <strong>{config.identity.provider === null ? 'none — one local user' : 'configured'}</strong>
        <br />
        Local display name: <code>{config.user.displayName}</code>{' '}
        <span className="quiet">used when no identity provider is configured</span>
        <br />
        <SourceBadge source={sources.identity} path={path} />
      </p>
      <p className="quiet">{DESK_FILE_NOTE}</p>
      <p className="quiet">
        The slot is one nullable field: <code>identity.provider</code> is null or an object. There
        is no <code>kind</code>, no vendor string and no third shape, and there is no{' '}
        <code>clientSecret</code> key in the schema — a secret pasted into the file is refused by
        name rather than silently persisted. An issuer someone else operates and an issuer you run
        are the same object with a different URL in it.
      </p>
      <p className="quiet">
        <strong>Configuring a provider gates nothing.</strong> Identity is display in every phase
        of this design; access stays the loopback bind, the session token this tab holds, and the
        origin check.
      </p>

      <h2 id={ADMIN_SECTIONS[2]!.id} className="section-title">
        {ADMIN_SECTIONS[2]!.title}
      </h2>
      <p>
        Connected runtime:{' '}
        {server ? (
          <>
            <code>{server.name}</code> {server.version}
          </>
        ) : (
          'not connected'
        )}
        <br />
        Tool listing: {known ? 'read' : 'not read on this connection'}
      </p>
      <p className="quiet">
        The runtime binary and the project directory are what the process was started with. Neither
        is in the configuration schema: the chassis executes the binary it was given, so a
        config-supplied path would be a way to run code on this machine by editing a file.
      </p>

      <h2 id={ADMIN_SECTIONS[3]!.id} className="section-title">
        {ADMIN_SECTIONS[3]!.title}
      </h2>
      <p>
        Configuration the runtime resolved:{' '}
        {data?.configPath ? <code>{data.configPath}</code> : <span className="quiet">not read yet</span>}
        <br />
        Desk configuration file: <code>{path}</code>{' '}
        <span className="quiet">read through the chassis file API, like any project file</span>
      </p>
      {note !== undefined && problems.length === 0 && readFailure === undefined && (
        <p className="quiet">
          {/* The ordinary case, said out loud rather than left as a silence
              indistinguishable from a file that was read and did nothing. */}
          <code>{note}</code>
        </p>
      )}

      <h2 id={ADMIN_SECTIONS[4]!.id} className="section-title">
        {ADMIN_SECTIONS[4]!.title}
      </h2>
      <p>
        Theme: <code>{config.appearance.theme}</code>
        <br />
        Density: <code>{config.appearance.density}</code>
        <br />
        <SourceBadge source={sources.appearance} path={path} />
      </p>
      <p className="quiet">
        <code>theme</code> is applied: <code>light</code> and <code>dark</code> write{' '}
        <code>data-theme</code> on the root element and <code>system</code> takes it off, leaving{' '}
        <code>prefers-color-scheme</code> to answer. <strong>What it selects today is a palette
        whose values are the light ones.</strong> Phase A ships that plumbing — the two selectors
        and the attribute — and none of the dark values: the three condition verdict colours carry
        meaning and cannot be mechanically inverted, and a desk that re-authored its neutrals
        around them would be half dark. So choosing dark changes the attribute and no colour, and
        the palette is its own piece of work.
      </p>
      <p className="quiet">
        <code>density</code> is recorded and validated and is read by nothing yet. It is in the
        schema because a file that carries it should not be refused for it; it is named here
        because a key that is accepted and does nothing should say so.
      </p>

      <h2 id={ADMIN_SECTIONS[5]!.id} className="section-title">
        {ADMIN_SECTIONS[5]!.title}
      </h2>
      <p>
        {/* **Configured, and labelled as configured.** These are the decoded
            numbers before the sheet's viewport caps touch them; the rendered
            column beside them is what is on screen. Printing one and calling
            it the other is how an accepted 720px Inspector was reported as
            720px while rendering 440px. */}
        Rail: <code>{config.panes.left.mode}</code>, configured{' '}
        <strong>{config.panes.left.width}px</strong> — rendered{' '}
        <Rendered box={rendered.rail} axis="width" />
        <br />
        Inspector: {config.panes.inspector.open ? 'open' : 'closed'}, configured{' '}
        <strong>{config.panes.inspector.width}px</strong> — rendered{' '}
        <Rendered box={rendered.inspector} axis="width" />
        <br />
        Console: {config.panes.console.open ? 'open' : 'closed'}, configured{' '}
        <strong>{config.panes.console.height}px</strong> — rendered{' '}
        <Rendered box={rendered.console} axis="height" />
        <br />
        <SourceBadge source={sources.panes} path={path} />
      </p>
      <p className="quiet">
        <strong>Configured is not rendered</strong>, and the difference is the frame rather than
        a rounding. The rendered figures above are measured off this page&apos;s own live panes;
        a pane that is not on screen at this width says so rather than reporting a number nothing
        has.
      </p>
      <p className="quiet">
        Each dimension is accepted only inside its range, inclusive at both ends, and a value
        outside it refuses the whole file by name:
        <br />
        {PANE_DIMENSIONS.map((dimension) => (
          <code key={dimension.key} className="partial-reason">
            {dimension.key}: {PANE_BOUNDS[dimension.key]!.min}–{PANE_BOUNDS[dimension.key]!.max}px
          </code>
        ))}
      </p>
      <p className="quiet">
        A legal value is then <strong>capped against the viewport it is actually in</strong>,
        because a size that fits a monitor can still eat a phone and this frame does not scroll.
        The rail and the Inspector each take at most <code>40vw</code>, which leaves the routes at
        least 20% of the width with both open. The console takes at most what leaves{' '}
        <code>120px</code> of route under the header and above this strip — <em>except</em> that
        an open console never falls below <code>80px</code>, the smallest height the schema
        accepts for one. On a viewport too short for both, the routes give way rather than the
        console silently becoming a pane of no height with a toggle still saying it is open;
        and where there is less room between the header and this strip than <code>80px</code>,
        the console takes all of it and no more, because the strip is the one thing that never
        leaves the frame.
      </p>
      <p className="quiet">
        Which panes are open is remembered per project on this machine only, under{' '}
        <code>{shell.storageKey}</code>
        {shell.keyResolved ? (
          '. '
        ) : (
          <span className="quiet">
            {' '}
            — provisional, because the chassis has not yet reported this project&apos;s root;
            nothing is written under it.{' '}
          </span>
        )}
        It is never sent anywhere and never written to the project. Phase A stores the collapse
        flags and the console&apos;s channel for the panes the viewer has actually moved, and no
        sizes at all, because nothing on this desk can yet change a size.
      </p>
      <p>
        <button type="button" onClick={() => setReset(shell.resetPanes())}>
          Reset panes on this machine
        </button>{' '}
        {/* What happened, not what was attempted. The reset runs inside the
            provider that owns the record — it cancels a write already on its
            way, refuses to clear the provisional key before the chassis has
            said which project this is, and reads the key back afterwards — and
            each of those is a different sentence here. */}
        {reset === 'cleared' && (
          <span className="quiet">Cleared, and the panes are back on their configured defaults.</span>
        )}
        {reset === 'refused' && (
          <span className="quiet">
            this browser did not clear the record — the layout is unchanged
          </span>
        )}
        {reset === 'unresolved' && (
          <span className="quiet">
            nothing was cleared: this desk has not yet been told which project it is open on, so
            the record above is not the one this project will use
          </span>
        )}
      </p>

      <Section title="The whole file">
        <PasteBlock
          label="jpack-desk.json, every key this location accepts"
          json={{
            deskConfigVersion: 1,
            organization: { name: 'Acme Co.', mark: null },
            user: { displayName: 'local user' },
            appearance: { theme: 'system', density: 'comfortable' },
            panes: {
              left: { mode: 'expanded', width: 248 },
              inspector: { open: false, width: 360 },
              console: { open: false, height: 240 }
            }
          }}
        />
      </Section>
    </article>
  )
}

/** The three bounded dimensions, in the order the section prints them. */
const PANE_DIMENSIONS = [
  { key: 'panes.left.width' },
  { key: 'panes.inspector.width' },
  { key: 'panes.console.height' }
] as const

/**
 * One measured dimension, or the reason there is not one.
 *
 * Three answers and not two. **Absent** is a pane that is not in the document
 * at this width — the Inspector's drawer form while it is closed — and
 * **collapsed** is one that is mounted at zero, which is what `hidden` plus
 * `display: none` produces. Reporting either as `0px` would be a measurement
 * of something that is not there.
 */
function Rendered({ box, axis }: { box: MeasuredBox | undefined; axis: 'width' | 'height' }) {
  if (box === undefined) return <span className="quiet">not mounted at this width</span>
  const value = box[axis]
  if (value === 0) return <span className="quiet">collapsed</span>
  return <strong>{value}px</strong>
}

function SourceBadge({ source, path }: { source: string; path: string }) {
  return (
    <span className="quiet">
      source: {source}
      {source === 'project file' ? (
        <>
          {' · '}
          <code>{path}</code>
        </>
      ) : (
        <>
          {' · no value for this section was read from '}
          <code>{path}</code>
        </>
      )}
    </span>
  )
}

/**
 * The exact JSON to paste, and a button that copies it.
 *
 * The button reports what happened rather than what it attempted. There is no
 * `navigator.clipboard` in an insecure context and the write can be refused by
 * permission, and a page whose whole argument is that it never states what it
 * did not observe cannot say "copied" on the strength of having asked.
 */
function PasteBlock({ label, json }: { label: string; json: unknown }) {
  const [copied, setCopied] = useState<boolean | undefined>(undefined)
  const text = JSON.stringify(json, null, 2)
  return (
    <div>
      <Json value={json} label={label} />
      <button
        type="button"
        onClick={() => {
          const written = navigator.clipboard?.writeText(text)
          if (!written) {
            setCopied(false)
            return
          }
          written.then(
            () => setCopied(true),
            () => setCopied(false)
          )
        }}
      >
        <IconCopy /> Copy
      </button>{' '}
      {copied === true && <span className="quiet">copied</span>}
      {copied === false && (
        <span className="quiet">this browser did not allow the copy — the JSON is above</span>
      )}
    </div>
  )
}
