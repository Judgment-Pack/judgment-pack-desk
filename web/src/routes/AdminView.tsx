/**
 * Admin: the settings, one shape, and no paragraph telling anyone how to read
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
 * config-supplied path would be a local-code-execution surface. The status
 * line reports what the process was started with.
 *
 * **Runtime and Panes are gone, and what went with each of them is the point.**
 * The Runtime card was status rather than settings: it is the line above, and
 * its content is in Help & About. The Panes card offered three pane dimensions
 * and a reset of this browser's own record of the layout — the dimensions were
 * a settings page editing the frame it is drawn in, and the reset is now in the
 * user menu, beside the panes it clears. Nothing about the schema changed: a
 * file with a `panes` member is still read, still applied and still validated;
 * what left is the settings UI for it, and its write path left with it.
 */
import { AssistantSection } from '../assistant/AssistantSection'
import { AdminStatusLine } from '../admin/AdminStatusLine'
import { CardField, SourceCard, SourceGroup, type SourceStatus } from '../admin/SourceCard'
import { useDefaultProject } from '../admin/DefaultProject'
import { AppearanceForm, OrganizationForm, StorageForm } from '../admin/projectFileCards'
import { useHashTarget } from '../shell/useHashTarget'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  type ConfigProblem,
  type DeskConfig,
  type EffectiveConfig,
  type ValueSource
} from '../config/deskConfig'
import { useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { ADMIN_GROUPS, ADMIN_SECTIONS } from './adminSections'

/** The sections, by id, so a card names its own rather than an index. */
const SECTION = Object.fromEntries(
  ADMIN_SECTIONS.map((section) => [section.id, section])
) as Record<string, { id: string; title: string }>

/** The groups, by id, on the same terms. */
const GROUP = Object.fromEntries(
  ADMIN_GROUPS.map((group) => [group.id, group])
) as Record<string, { id: string; title: string }>

export function AdminView() {
  const effective = useEffectiveConfig()
  const { config, desk } = effective
  const mcp = useMcp()
  const listing = useFileListing()
  // The project group's one field and its Save, sharing one draft across two
  // of the header's slots.
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

      {/* Not a card, because none of it is a setting: what the desk is
          connected to and where its two files are, from the chassis' own
          answers. */}
      <AdminStatusLine
        runtime={runtimeSays(mcp)}
        binary={runtimeBinary(effective)}
        projectFile={projectLocation(effective)}
        deskFile={deskLocation(effective)}
      />

      <SourceGroup
        id={GROUP['this-project']!.id}
        title={GROUP['this-project']!.title}
        location={projectLocation(effective)}
        status={projectStatus(effective)}
        // The whole file, and only where it was accepted: the group's own
        // Status is what gates it, and a refused document is exactly the one
        // that must not be rendered.
        content={{ text: effective.text }}
        // The one control that writes the *desk-level* file from here: it
        // nominates this project as the default, or withdraws one. Its own
        // line names the file it writes, which is not the file above it.
        fields={defaultProject.field}
        save={defaultProject.save}
      >
        <SourceCard
          id={SECTION.organization!.id}
          title={SECTION.organization!.title}
          location={sectionLocation(effective, 'organization')}
          status={sectionStatus(effective, 'organization')}
          under={groupFor(effective, 'organization')}
          content={{
            text: textFor(effective, 'organization'),
            member: 'organization',
            value: config.organization
          }}
          save={<OrganizationForm />}
        />

        <SourceCard
          id={SECTION.storage!.id}
          title={SECTION.storage!.title}
          location={sectionLocation(effective, 'storage')}
          status={sectionStatus(effective, 'storage')}
          under={groupFor(effective, 'storage')}
          content={{
            text: textFor(effective, 'storage'),
            member: 'storage',
            value: config.storage
          }}
          save={<StorageForm dirSays={PACK_LOCATION_SAYS[packLocation]} />}
        />

        <SourceCard
          id={SECTION.appearance!.id}
          title={SECTION.appearance!.title}
          location={sectionLocation(effective, 'appearance')}
          status={sectionStatus(effective, 'appearance')}
          under={groupFor(effective, 'appearance')}
          content={{
            text: textFor(effective, 'appearance'),
            member: 'appearance',
            value: config.appearance
          }}
          save={<AppearanceForm />}
        />
      </SourceGroup>

      <SourceGroup
        id={GROUP['this-desk']!.id}
        title={GROUP['this-desk']!.title}
        location={deskLocation(effective)}
        status={deskStatus(effective)}
      >
        <AssistantSection
          id={SECTION.assistant!.id}
          title={SECTION.assistant!.title}
          under={deskStatus(effective)}
        />

        <SourceCard
          id={SECTION['identity-provider']!.id}
          title={SECTION['identity-provider']!.title}
          location={deskLocation(effective)}
          status={deskStatus(effective)}
          under={deskStatus(effective)}
          content={{ text: desk?.text, member: 'identity', value: config.identity }}
          fields={
            <CardField label="Provider">
              {config.identity.provider === null ? (
                'None'
              ) : (
                <>
                  <code>{config.identity.provider.issuer}</code>
                  {config.identity.provider.label !== null && (
                    <> — {config.identity.provider.label}</>
                  )}
                </>
              )}
            </CardField>
          }
        />
      </SourceGroup>

    </article>
  )
}

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

/**
 * The connection, in the connection's own words.
 *
 * One producer, because the status line and Help & About both say it and two
 * sentences about one socket are free to disagree about whether it is up.
 */
function runtimeSays(mcp: ReturnType<typeof useMcp>): string {
  const { server } = mcp
  return server ? `connected — ${server.name} ${server.version}` : 'not connected'
}

/** The binary the desk was launched with, as the chassis reported it. */
function runtimeBinary(effective: EffectiveConfig) {
  const chassis = effective.desk?.chassis
  if (chassis === undefined) return <span className="quiet">the desk has not said</span>
  return <code>{chassis.runtimeBin}</code>
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
 * The group header a layered section sits under, where its own file is the
 * group's — and **nothing** where it is not.
 *
 * A section that came from the desk-level file is not one the project group's
 * header speaks for: the header names this project's file, and a card that
 * dropped its Location under it would be attributing a value to a file it did
 * not come from. So that card is given no group at all and states its own
 * Location and Status, exactly as it did before there were groups.
 */
function groupFor(
  effective: EffectiveConfig,
  section: LayeredSection
): SourceStatus | undefined {
  return effective.sources[section] === 'desk file' ? undefined : projectStatus(effective)
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
