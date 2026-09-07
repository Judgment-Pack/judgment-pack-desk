/**
 * Admin: eight cards, one shape, and no paragraph telling anyone how to read
 * them.
 *
 * **The narration is gone, and its removal is the change.** Every section used
 * to carry one or two paragraphs, a standing disclaimer, warning notes and a
 * paste block, and a reader who wanted the same four facts about two settings
 * had to find them in two shapes. What is left is the four facts, in one order,
 * per section: **where** the value is written, **whether** it was read, **what**
 * is in the file, and the fields — with a Save on the one card that has a write
 * path. A real problem is the card's Status line and nothing else.
 *
 * **A location is never composed here.** The desk-level file's path, the
 * project file's path and the runtime binary come from the chassis; a page that
 * joined a directory to a file name would be asserting a location on a
 * filesystem it cannot see.
 *
 * `runtime` and the project root are **not in the schema**, and that is the
 * design rather than a gap: `relay.go` runs the configured binary, so a
 * config-supplied path would be a local-code-execution surface. The Runtime
 * card reports what the process was started with.
 */
import { useState } from 'react'
import { AssistantSection } from '../assistant/AssistantSection'
import { CardField, SourceCard, type SourceStatus } from '../admin/SourceCard'
import { useDefaultProject } from '../admin/DefaultProject'
import { useHashTarget } from '../shell/useHashTarget'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  DESK_FALLBACK_NAME,
  PANE_BOUNDS,
  type ConfigProblem,
  type DeskConfig,
  type EffectiveConfig,
  type ValueSource
} from '../config/deskConfig'
import { useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { usePacks } from '../mcp/queries'
import { useRenderedPanes, type MeasuredBox } from '../shell/measured'
import { useShellState } from '../shell/paneState'
import type { ResetOutcome } from '../shell/paneState'
import { ADMIN_SECTIONS } from './adminSections'

/** The sections, by id, so a card names its own rather than an index. */
const SECTION = Object.fromEntries(
  ADMIN_SECTIONS.map((section) => [section.id, section])
) as Record<string, { id: string; title: string }>

export function AdminView() {
  const effective = useEffectiveConfig()
  const { config, desk } = effective
  const { server, known } = useMcp()
  const { data } = usePacks()
  const listing = useFileListing()
  const shell = useShellState()
  // Re-measured when a pane is toggled: a pane arriving or leaving is not a
  // resize of anything already observed.
  const rendered = useRenderedPanes(
    `${shell.left.mode}|${shell.inspector.open}|${shell.console.open}`
  )
  const [reset, setReset] = useState<ResetOutcome | undefined>(undefined)
  // The Project card's one field and its Save, sharing one draft across two of
  // the card's slots.
  const defaultProject = useDefaultProject()
  // The rail's and the user menu's section links carry a hash. Nothing in the
  // router scrolls to one, and the document is not the scroll container here —
  // `.desk-main` is — so without this they changed the URL and moved nothing.
  useHashTarget()

  const packDir = config.storage.packs.dir
  const packLocation = packLocationState(packDir, listing)

  return (
    <article className="detail">
      <header className="detail-head">
        <h1>Admin</h1>
      </header>

      <SourceCard
        id={SECTION.project!.id}
        title={SECTION.project!.title}
        location={projectLocation(effective)}
        status={projectStatus(effective)}
        content={{ text: effective.text, value: {} }}
        fields={defaultProject.field}
        save={defaultProject.save}
      />

      <SourceCard
        id={SECTION['identity-provider']!.id}
        title={SECTION['identity-provider']!.title}
        location={deskLocation(effective)}
        status={deskStatus(effective)}
        content={{ text: desk?.text, member: 'identity', value: config.identity }}
        fields={
          <CardField label="Provider">
            {config.identity.provider === null ? (
              'None'
            ) : (
              <>
                <code>{config.identity.provider.issuer}</code>{' '}
                {config.identity.provider.label ?? ''}
              </>
            )}
          </CardField>
        }
      />

      <AssistantSection id={SECTION.assistant!.id} title={SECTION.assistant!.title} />

      <SourceCard
        id={SECTION.runtime!.id}
        title={SECTION.runtime!.title}
        location={
          desk?.chassis === undefined ? (
            <span className="quiet">the desk has not said</span>
          ) : (
            <code>{desk.chassis.runtimeBin}</code>
          )
        }
        status={{
          state: 'said',
          says: server
            ? `connected — ${server.name} ${server.version}`
            : 'not connected'
        }}
        content={{
          value: {
            server: server ?? null,
            toolListing: known ? 'read' : 'not read on this connection'
          }
        }}
        fields={
          <CardField label="Configuration">
            {data?.configPath ? (
              <code>{data.configPath}</code>
            ) : (
              <span className="quiet">not read yet</span>
            )}
          </CardField>
        }
      />

      <SourceCard
        id={SECTION.storage!.id}
        title={SECTION.storage!.title}
        location={sectionLocation(effective, 'storage')}
        status={sectionStatus(effective, 'storage')}
        content={{
          text: textFor(effective, 'storage'),
          member: 'storage',
          value: config.storage
        }}
        fields={
          <>
            <CardField label="Kind">
              <strong>{config.storage.packs.kind}</strong>
            </CardField>
            <CardField label="Packs go to" rule={PACK_LOCATION_SAYS[packLocation]}>
              <code>{config.storage.packs.dir}</code>
            </CardField>
            <CardField label="Id prefix">
              <code>{config.storage.packs.idBase}</code>
            </CardField>
            <CardField label="Not available yet">
              <span>database — coming soon</span>
              {', '}
              <span>cloud storage — coming soon</span>
            </CardField>
          </>
        }
      />

      <SourceCard
        id={SECTION.organization!.id}
        title={SECTION.organization!.title}
        location={sectionLocation(effective, 'organization')}
        status={sectionStatus(effective, 'organization')}
        content={{
          text: textFor(effective, 'organization'),
          member: 'organization',
          value: config.organization
        }}
        fields={
          <>
            <CardField label="Name">
              <strong>
                {config.organization.name ?? `${DESK_FALLBACK_NAME} (no name configured)`}
              </strong>
            </CardField>
            <CardField label="Mark">
              {config.organization.mark ? 'configured in the file' : 'none — a monogram'}
            </CardField>
          </>
        }
      />

      <SourceCard
        id={SECTION.appearance!.id}
        title={SECTION.appearance!.title}
        location={sectionLocation(effective, 'appearance')}
        status={sectionStatus(effective, 'appearance')}
        content={{
          text: textFor(effective, 'appearance'),
          member: 'appearance',
          value: config.appearance
        }}
        fields={
          <>
            <CardField label="Theme" rule="Applied. The palette it selects is the light one.">
              <code>{config.appearance.theme}</code>
            </CardField>
            <CardField label="Density" rule="Accepted and read by nothing yet.">
              <code>{config.appearance.density}</code>
            </CardField>
          </>
        }
      />

      <SourceCard
        id={SECTION.panes!.id}
        title={SECTION.panes!.title}
        location={sectionLocation(effective, 'panes')}
        status={sectionStatus(effective, 'panes')}
        content={{ text: textFor(effective, 'panes'), member: 'panes', value: config.panes }}
        fields={
          <>
            <CardField label="Rail">
              {/* **Configured, and labelled as configured.** These are the
                  decoded numbers before the sheet's viewport caps touch them;
                  the rendered figure beside them is what is on screen. Printing
                  one and calling it the other is how an accepted 720px
                  Inspector was reported as 720px while rendering 440px. */}
              <code>{config.panes.left.mode}</code>, configured{' '}
              <strong>{config.panes.left.width}px</strong> — rendered{' '}
              <Rendered box={rendered.rail} axis="width" />
            </CardField>
            <CardField label="Inspector">
              {config.panes.inspector.open ? 'open' : 'closed'}, configured{' '}
              <strong>{config.panes.inspector.width}px</strong> — rendered{' '}
              <Rendered box={rendered.inspector} axis="width" />
            </CardField>
            <CardField label="Console">
              {config.panes.console.open ? 'open' : 'closed'}, configured{' '}
              <strong>{config.panes.console.height}px</strong> — rendered{' '}
              <Rendered box={rendered.console} axis="height" />
            </CardField>
            <CardField label="Accepted ranges">
              {PANE_DIMENSIONS.map((dimension) => (
                <code key={dimension.key} className="partial-reason">
                  {dimension.key}: {PANE_BOUNDS[dimension.key]!.min}–
                  {PANE_BOUNDS[dimension.key]!.max}px
                </code>
              ))}
            </CardField>
            <CardField label="Remembered under">
              <code>{shell.storageKey}</code>{' '}
              {!shell.keyResolved && (
                <span className="quiet">provisional — this project&apos;s root is not known</span>
              )}
            </CardField>
          </>
        }
        save={
          <p className="actions">
            <button type="button" onClick={() => setReset(shell.resetPanes())}>
              Reset panes on this machine
            </button>{' '}
            {/* What happened, not what was attempted. The reset runs inside
                the provider that owns the record — it cancels a write already
                on its way, refuses to clear the provisional key before the
                chassis has said which project this is, and reads the key back
                afterwards — and each of those is a different sentence. */}
            {reset === 'cleared' && (
              <span className="quiet">Cleared — the panes are back on their defaults.</span>
            )}
            {reset === 'refused' && (
              <span className="quiet">
                this browser did not clear the record — the layout is unchanged
              </span>
            )}
            {reset === 'unresolved' && (
              <span className="quiet">
                nothing was cleared: this desk has not been told which project it is open on
              </span>
            )}
          </p>
        }
      />
    </article>
  )
}

/** The three bounded dimensions, in the order the section prints them. */
const PANE_DIMENSIONS = [
  { key: 'panes.left.width' },
  { key: 'panes.inspector.width' },
  { key: 'panes.console.height' }
] as const

/** The sections that come from either file, layered. */
type LayeredSection = Exclude<
  keyof DeskConfig,
  'deskConfigVersion' | 'identity' | 'assistant' | 'project'
>

/**
 * Where the project's own configuration file is, **as the chassis said it**.
 *
 * The absolute path the chassis resolved, and the project-relative name only
 * where it has not answered — which is honest about being a name rather than a
 * location. Joining the reported directory to a file name here would be this
 * page composing a path on a filesystem it cannot see, and would be wrong the
 * first time a project was reached through a symlink.
 */
function projectLocation(effective: EffectiveConfig) {
  const chassis = effective.desk?.chassis
  if (chassis === undefined) return <code>{effective.path}</code>
  return <code>{chassis.projectFile}</code>
}

/** Where the desk-level file is, as the chassis said it — or that nothing asked. */
function deskLocation(effective: EffectiveConfig) {
  if (effective.desk === undefined) {
    return <span className="quiet">nothing has asked for it</span>
  }
  return <code>{effective.desk.path}</code>
}

/** Which file supplied one layered section, and therefore where it is written. */
function sectionLocation(effective: EffectiveConfig, section: LayeredSection) {
  const source: ValueSource = effective.sources[section]
  if (source === 'desk file') return deskLocation(effective)
  return projectLocation(effective)
}

/** The bytes of whichever file supplied one layered section. */
function textFor(effective: EffectiveConfig, section: LayeredSection): string | undefined {
  return effective.sources[section] === 'desk file' ? effective.desk?.text : effective.text
}

/**
 * The project file's own state.
 *
 * Four answers and not two. A refused file is not an absent one, and a read
 * that never produced a file establishes only that absence was **not**
 * established — which is weaker than either and is said as such.
 */
function projectStatus(effective: EffectiveConfig): SourceStatus {
  if (effective.problems.length > 0) return refused(effective.problems)
  if (effective.readFailure !== undefined) {
    return { state: 'unread', failure: effective.readFailure }
  }
  if (effective.note !== undefined) return { state: 'absent' }
  return { state: 'read' }
}

/** The desk-level file's own state, on the same four terms plus "nothing asked". */
function deskStatus(effective: EffectiveConfig): SourceStatus {
  const desk = effective.desk
  if (desk === undefined) return { state: 'pending' }
  if (desk.problems.length > 0) return refused(desk.problems)
  if (desk.readFailure !== undefined) return { state: 'unread', failure: desk.readFailure }
  if (!desk.present) return { state: 'absent' }
  return { state: 'read' }
}

/**
 * One layered section's state: the state of the file that supplied it, or —
 * where neither did — what the project file has to say about not carrying it.
 */
function sectionStatus(effective: EffectiveConfig, section: LayeredSection): SourceStatus {
  const source: ValueSource = effective.sources[section]
  if (source === 'desk file') return { state: 'read' }
  if (source === 'project file') return { state: 'read' }
  const project = projectStatus(effective)
  return project.state === 'read' ? { state: 'absent' } : project
}

function refused(problems: ConfigProblem[]): SourceStatus {
  return { state: 'refused', problems }
}

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

/**
 * What is known about the configured pack location, as one of six states.
 *
 * These were two — "holds files" and "no file under it yet, the first pack
 * creates it" — and the second was said about a listing that had not answered,
 * one that failed, one that came back incomplete, and one where a regular file
 * sits exactly at the location and *nothing* can be created inside it. Four
 * different facts, one sentence, and three of them false.
 *
 * The listing reports **regular files only**, so an empty directory is
 * invisible to it: "absent" here means "no file is under it", which is the
 * honest limit of the evidence and is worded as such. What the listing *can*
 * see, and what nothing was reading, is a regular file at the location itself.
 */
type PackLocation = 'pending' | 'failed' | 'partial' | 'obstructed' | 'holds-files' | 'no-file-under-it'

const PACK_LOCATION_SAYS: Record<PackLocation, string> = {
  pending: 'the file listing has not answered yet',
  failed: 'the file listing failed, so nothing is known about it',
  partial: 'the file listing came back incomplete, so nothing is known about it',
  obstructed: 'a file is there under that exact name — nothing can be created inside it',
  'holds-files': 'holds files',
  'no-file-under-it': 'no file is under it — the first pack asks for it to be created'
}

function packLocationState(
  dir: string,
  listing: { isPending: boolean; isError: boolean; data?: { files: { path: string }[]; partial?: string[] } }
): PackLocation {
  if (listing.isPending) return 'pending'
  if (listing.isError) return 'failed'
  const files = listing.data?.files ?? []
  // A regular file exactly at the location. The listing sees this and nothing
  // was asking it, so Admin promised the first pack would create a directory
  // that a rename is the only way to get.
  if (files.some((file) => file.path === dir)) return 'obstructed'
  if (files.some((file) => file.path.startsWith(`${dir}/`))) return 'holds-files'
  // Only now does absence mean anything, and only because the listing is
  // complete: a partial one is explicitly not all the files.
  if ((listing.data?.partial ?? []).length > 0) return 'partial'
  return 'no-file-under-it'
}
