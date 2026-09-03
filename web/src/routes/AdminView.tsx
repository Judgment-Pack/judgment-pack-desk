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
 * page can do. The only control that **changes any state at all** is Panes'
 * reset, which clears one `localStorage` key; the Copy buttons beside each
 * paste block put text on the clipboard and change nothing here.
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
import { DESK_FALLBACK_NAME } from '../config/deskConfig'
import { useMcp } from '../mcp/McpProvider'
import { usePacks } from '../mcp/queries'
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
          read did not succeed, so whatever it says has not been applied. The reason is the
          chassis' own, verbatim.
          <br />
          <code className="partial-reason">{readFailure}</code>
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
        Rail: <code>{config.panes.left.mode}</code>, {config.panes.left.width}px
        <br />
        Inspector: {config.panes.inspector.open ? 'open' : 'closed'}, {config.panes.inspector.width}px
        <br />
        Console: {config.panes.console.open ? 'open' : 'closed'}, {config.panes.console.height}px
        <br />
        <SourceBadge source={sources.panes} path={path} />
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
