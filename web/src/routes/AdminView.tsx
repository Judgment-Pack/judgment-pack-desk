/**
 * Admin: a navigation column, one open section, and the file itself in the
 * right pane.
 *
 * **There is no overview, and its absence is this file's argument.** The page
 * was a stack of four sections, then a stack *and* an overview of it — a
 * landing state that was a second page of the same list, reachable at `#all`,
 * linked from every open section, and returned to by Escape. A settings page is
 * a navigation column and a section that is open: macOS System Settings has no
 * "all settings" pane, and neither does Linear's. What the overview carried
 * that the column cannot — where each file is, what reading it produced, and
 * the one control that is about the project rather than a member of it — is a
 * **section** now, first under This project, and the right pane says the rest.
 *
 * **A row is a link and the hash is the state.** `/admin#assistant` opens the
 * assistant section, and it did nothing but scroll before; the rail's menu and
 * the user menu have linked to these fragments since they were headings, so the
 * addresses are the ones already in circulation. **A fragment that names no
 * section opens the first one** — as does no fragment at all, and one that is
 * not valid percent-encoding. There is no state in which nothing is open, so
 * there is nothing an error page or a back link would be for.
 *
 * **The bytes went to the right pane.** They are context and not a setting:
 * nobody edits a file's text here, and the disclosure that held it was one more
 * thing stacked into a scroll that already had four sections in it. The page
 * claims the Inspector slot the way the pack routes do — `useInspectorPortal`,
 * a claim held for as long as the route is mounted and released when it leaves
 * — so the file is *beside* the form instead of underneath it. Nothing about
 * what may be shown changed: a refused file's bytes are still never rendered,
 * and the gate is the card's own `showsContent`, imported rather than spelled
 * again.
 *
 * **The forms stay in the main column**, and that is a measurement rather than
 * a preference: the pane is 360px, the label column alone is 9rem, and below
 * 1100px the pane is a drawer that covers the page it would be editing. A form
 * in a drawer over its own page is a worse answer than a form on the page.
 *
 * **A location is never composed here, and never stood in for.** The desk-level
 * file's path, the project file's path and the runtime binary come from the
 * chassis; a page that joined a directory to a file name would be asserting a
 * location on a filesystem it cannot see, and one that fell back to the
 * relative name it reads the file by would be offering a file-API address as
 * an established location. Where the chassis has not answered, the row says
 * so.
 *
 * The shell's wide measure leaves room for navigation beside the form. Padded
 * links and an accent tint identify the open section; headings and spacing
 * separate the fields. Each section keeps one primary Save action.
 *
 * `runtime` and the project root are **not in the schema**, and that is the
 * design rather than a gap: `relay.go` runs the configured binary, so a
 * config-supplied path would be a local-code-execution surface. The status
 * line reports what the process was started with.
 */
import { useEffect, useRef, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { AssistantSection } from '../assistant/AssistantSection'
import { AdminStatusLine } from '../admin/AdminStatusLine'
import { ConfigPane } from '../admin/ConfigPane'
import { SECTION_SUMMARY } from '../admin/sectionSummary'
import { CardField, SourceCard, StatusLine, type SourceStatus } from '../admin/SourceCard'
import { useDefaultProject } from '../admin/DefaultProject'
import { OrganizationForm, StorageForm, StorageKind } from '../admin/projectFileCards'
import { useHashTarget } from '../shell/useHashTarget'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { INSPECTOR_DRAWER_BELOW, useMediaQuery } from '../shell/useMediaQuery'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  type ConfigProblem,
  type DeskConfig,
  type EffectiveConfig,
  type ValueSource
} from '../config/deskConfig'
import { useFileListing } from '../files/queries'
import { connectionSays, useMcp } from '../mcp/McpProvider'
import { ADMIN_GROUPS, ADMIN_SECTIONS, type AdminSection } from './adminSections'
import styles from './AdminView.module.css'

/** The sections, by id, so a row names its own rather than an index. */
const SECTION = Object.fromEntries(
  ADMIN_SECTIONS.map((section) => [section.id, section])
) as Record<string, AdminSection | undefined>

/**
 * Which member of which file each section is about.
 *
 * The section id and the member are **not** the same string — `identity` is
 * written by a section called Identity provider — so the pane is told the
 * member rather than deriving one from a title.
 */
const SECTION_MEMBER: Record<string, keyof DeskConfig> = {
  organization: 'organization',
  storage: 'storage',
  assistant: 'assistant',
  'identity-provider': 'identity'
}

/** The two sections that exist only in the desk-level file. */
const DESK_ONLY = new Set(['assistant', 'identity-provider'])

/**
 * The one section that is about the project's file itself rather than about a
 * member of it — so it is in `ADMIN_SECTIONS` and in neither map above.
 */
const PROJECT_FILE_SECTION = 'project'

export function AdminView() {
  const effective = useEffectiveConfig()
  const { config } = effective
  const mcp = useMcp()
  const listing = useFileListing()
  // The Project section's one field and its Save, sharing one draft across two
  // of that section's slots.
  const defaultProject = useDefaultProject()
  const { hash } = useLocation()
  // Below 1100px the Inspector is a drawer and the shell is one column: the
  // list stacks above the open section, and the rows that are not open say
  // their titles and nothing else, because a summary each is a second page of
  // list above the thing the reader opened.
  const stacked = useMediaQuery(INSPECTOR_DRAWER_BELOW)
  // **And that is the one shell where the fragment scrolls.** The rail's and
  // the user menu's section links carry a hash, and the router scrolls to
  // none — but here the hash *opens* the section, and where the section is
  // beside the list rather than below it there is nothing to scroll to.
  // Scrolling anyway took the page's heading, its status line and the top of
  // the list off the screen, because `.desk-main` is the one scroll container
  // both columns are in.
  useHashTarget(stacked)
  const open = sectionFromHash(hash)
  const packDir = config.storage.packs.dir
  const packLocation = packLocationState(packDir, listing)

  // **And where it does not scroll, it starts at the top.** A full load of
  // `/admin#assistant` is scrolled by the browser itself — the shell's scroll
  // container is `.desk-main` and the browser scrolls the nearest one, which
  // the hook's own comment used to say it would not — and a click on a row
  // while a tall section is scrolled would otherwise open the next one halfway
  // down. Stated on this page's own element rather than on the shell's, so a
  // route is not selecting the frame it is rendered in.
  const top = useRef<HTMLElement | null>(null)
  useEffect(() => {
    if (stacked) return
    top.current?.scrollIntoView()
  }, [open.id, stacked])

  // **Nothing listens for Escape here, because there is nothing to leave.**
  // The key used to return to the overview; a page whose every state is one
  // open section has no state Escape could put a reader in, and a keystroke
  // that navigated to the first section would be a shortcut for "lose your
  // place".

  // **The pane, claimed for as long as this route is mounted.** The claim and
  // the portal are one call, so leaving Admin releases the slot and the next
  // route's own panel — or the pane's empty state — takes it back.
  const pane = useInspectorPortal(
    <ConfigPane {...paneFor(effective, open)} />
  )

  return (
    <article className={`detail ${styles.admin}`} data-measure="wide" ref={top}>
      {pane}
      <header className="detail-head">
        <div>
          <h1>Admin</h1>
          <p className={styles.description}>Manage your project and this workspace.</p>
        </div>
        <details className={styles.diagnostics}>
          <summary>Runtime details</summary>
          <AdminStatusLine runtime={runtimeSays(mcp)} binary={runtimeBinary(effective)} />
        </details>
      </header>

      <div className={styles.split}>
        <nav className={styles.rail} aria-label="Settings">
          {ADMIN_GROUPS.map((group) => (
            <GroupRows
              key={group.id}
              effective={effective}
              group={group}
              open={open}
              stacked={stacked}
            />
          ))}
        </nav>
        <div className={styles.open}>
          {/* The file itself, and the one control that is about the project
              rather than about a member of it. The two rows are the ones the
              group header carried while there was an overview to carry them
              on. */}
          {open.id === 'project' && (
            <SourceCard
              id={SECTION.project!.id}
              title={SECTION.project!.title}
              level={2}
              location={projectLocation(effective)}
              status={projectStatus(effective)}
              fields={defaultProject.field}
              save={defaultProject.save}
            />
          )}
          {open.id === 'organization' && (
            <SourceCard
              id={SECTION.organization!.id}
              title={SECTION.organization!.title}
              level={2}
              location={sectionLocation(effective, 'organization')}
              status={sectionStatus(effective, 'organization')}
              under={groupFor(effective, 'organization')}
              save={<OrganizationForm />}
            />
          )}
          {open.id === 'storage' && (
            <SourceCard
              id={SECTION.storage!.id}
              title={SECTION.storage!.title}
              level={2}
              location={sectionLocation(effective, 'storage')}
              status={sectionStatus(effective, 'storage')}
              under={groupFor(effective, 'storage')}
              fields={<StorageKind />}
              save={<StorageForm dirSays={PACK_LOCATION_SAYS[packLocation]} />}
            />
          )}
          {open.id === 'assistant' && (
            <AssistantSection
              id={SECTION.assistant!.id}
              title={SECTION.assistant!.title}
              level={2}
              under={deskStatus(effective)}
            />
          )}
          {open.id === 'identity-provider' && (
            <SourceCard
              id={SECTION['identity-provider']!.id}
              title={SECTION['identity-provider']!.title}
              level={2}
              location={deskLocation(effective)}
              status={deskStatus(effective)}
              under={deskStatus(effective)}
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
          )}
        </div>
      </div>
    </article>
  )
}

/**
 * One group of the navigation column: its title, and a row per section.
 *
 * **A title and rows, and never the file's own head.** The group header used to
 * state where its file is and what reading it produced, on an overview that no
 * longer exists; beside an open section a path is a line of prose in a 13rem
 * column, and the two places that fact belongs are the section that is about
 * that file and the pane that quotes it.
 */
function GroupRows({
  effective,
  group,
  open,
  stacked
}: {
  effective: EffectiveConfig
  group: (typeof ADMIN_GROUPS)[number]
  open: AdminSection
  stacked: boolean
}) {
  return (
    <div className={styles.railGroup}>
      <p className={styles.railTitle} id={`rail-${group.id}`}>
        {group.title}
      </p>
      <ul className={styles.rows} aria-labelledby={`rail-${group.id}`}>
        {group.sections.map((section) => (
          <SectionRow
            key={section.id}
            effective={effective}
            section={section}
            current={open.id === section.id}
            // "Collapse to their titles" is what the narrow shell does to the
            // rows a reader is not in: the list is above the open section there,
            // not beside it, and a summary each pushes the section off the
            // screen it was opened on.
            bare={stacked && open.id !== section.id}
          />
        ))}
      </ul>
    </div>
  )
}

/**
 * One row: the title, what the setting currently is, and the section's own
 * status where it differs from the state of the file its group is about.
 *
 * The summary is `SECTION_SUMMARY`'s and is drawn from the decoded value — the
 * page composes no part of it. The status is the same comparison a card makes:
 * by what the two say rather than by which state each is, because two refusals
 * naming two keys are not one status.
 */
function SectionRow({
  effective,
  section,
  current,
  bare
}: {
  effective: EffectiveConfig
  section: AdminSection
  current: boolean
  bare: boolean
}) {
  const summarise = SECTION_SUMMARY[section.id]
  const own = statusOfSection(effective, section.id)
  const group = groupStatusFor(effective, section.id)
  const differs = JSON.stringify(own) !== JSON.stringify(group)
  return (
    <li className={styles.rowItem}>
      <Link
        className={styles.row}
        to={`/admin#${section.id}`}
        aria-current={current ? 'true' : undefined}
      >
        <span className={styles.rowTitle}>
          {section.title}
        </span>
        {!bare && summarise !== undefined && (
          <span className={styles.rowSays}>{summarise(effective)}</span>
        )}
        {!bare && differs && (
          <span className={styles.rowStatus} data-state={own.state}>
            <StatusLine status={own} />
          </span>
        )}
      </Link>
    </li>
  )
}

/**
 * The section a fragment names, and the **first** section for every fragment
 * that names none.
 *
 * Three inputs and one answer: no fragment, a fragment naming no section — a
 * group id, a section that was renamed, a link somebody typed — and one that is
 * not valid percent-encoding. There is no fourth state for any of them to land
 * in: an error about a section that does not exist would be a worse answer than
 * the page the reader asked for, and the page they asked for is a settings page,
 * which opens on its first pane.
 */
function sectionFromHash(hash: string): AdminSection {
  const first = ADMIN_SECTIONS[0]!
  if (hash.length < 2) return first
  let id: string
  try {
    id = decodeURIComponent(hash.slice(1))
  } catch {
    return first
  }
  return SECTION[id] ?? first
}

/**
 * What the right pane is about: the whole project file under Project, and one
 * member of whichever file supplied it under every other section.
 *
 * **Project is the section that is about a file rather than a member of one**,
 * so its pane is the whole document — which is what the overview's pane was,
 * unchanged, safety rule included: a refused file shows its Status and no bytes
 * at all.
 *
 * The title names the file by the name this desk knows it by — the project's
 * own is the name it is read at, and the desk-level file's is the last segment
 * of the path the chassis reported. Where the chassis has named no file, the
 * title is the member alone rather than a file name this page made up.
 */
function paneFor(
  effective: EffectiveConfig,
  open: AdminSection
): {
  title: string
  location: ReactNode
  status: SourceStatus
  digest?: string
  text?: string
  member?: string
} {
  if (open.id === PROJECT_FILE_SECTION) {
    return {
      title: effective.path,
      location: projectLocation(effective),
      status: projectStatus(effective),
      digest: effective.sha256,
      text: effective.text
    }
  }
  const member = SECTION_MEMBER[open.id]!
  const fromDesk =
    DESK_ONLY.has(open.id) || effective.sources[member as LayeredSection] === 'desk file'
  const file = fromDesk ? fileName(effective.desk?.path) : effective.path
  return {
    title: file === undefined ? member : `${file} › ${member}`,
    location: fromDesk ? deskLocation(effective) : projectLocation(effective),
    status: fromDesk ? deskStatus(effective) : projectStatus(effective),
    digest: fromDesk ? effective.desk?.sha256 : effective.sha256,
    text: fromDesk ? effective.desk?.text : effective.text,
    member
  }
}

/** The last segment of a path the chassis reported, or nothing. */
function fileName(path: string | undefined): string | undefined {
  if (path === undefined) return undefined
  const at = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'))
  const name = at === -1 ? path : path.slice(at + 1)
  return name === '' ? undefined : name
}

/** The sections that come from either file, layered. */
type LayeredSection = Exclude<
  keyof DeskConfig,
  'deskConfigVersion' | 'identity' | 'assistant' | 'project'
>

/** One section's own state, whichever of the two files it belongs to. */
function statusOfSection(effective: EffectiveConfig, id: string): SourceStatus {
  if (id === PROJECT_FILE_SECTION) return projectStatus(effective)
  if (DESK_ONLY.has(id)) return deskStatus(effective)
  return sectionStatus(effective, SECTION_MEMBER[id] as LayeredSection)
}

/** The state the file this section's group is about is in, for comparison. */
function groupStatusFor(effective: EffectiveConfig, id: string): SourceStatus | undefined {
  if (id === PROJECT_FILE_SECTION) return projectStatus(effective)
  if (DESK_ONLY.has(id)) return deskStatus(effective)
  return groupFor(effective, SECTION_MEMBER[id] as LayeredSection)
}

/**
 * Where the project's own configuration file is, **as the chassis said it** —
 * or that it has not said.
 *
 * The absolute path the chassis resolved, and **nothing** where it has not
 * answered. It used to fall back to `effective.path`, the project-relative
 * name this page reads the file by: that is a file-API address rather than an
 * established location on a filesystem, and a Location row showing it was the
 * page answering a question only the chassis can answer — before
 * `/api/desk-config` has answered at all, and for ever where it never carries
 * chassis facts. Joining the reported directory to a file name here would be
 * the same mistake one step further on, and would be wrong the first time a
 * project was reached through a symlink.
 */
function projectLocation(effective: EffectiveConfig) {
  const chassis = effective.desk?.chassis
  if (chassis === undefined) return <span className="quiet">the desk has not said</span>
  return <code>{chassis.projectFile}</code>
}

/**
 * The connection, in the connection's own words.
 *
 * **The verdict is the status; the name is the metadata.** `server` is retained
 * across a reconnect, so a line that read "connected" off its presence said so
 * while the socket was down and the banner said otherwise. The runtime is named
 * only where the connection is actually up. See `connectionSays`.
 */
function runtimeSays(mcp: ReturnType<typeof useMcp>): string {
  const { status, server } = mcp
  const says = connectionSays(status)
  if (status !== 'ready' || server === null) return says
  return `${says} — ${server.name} ${server.version}`
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
 * The state of the file a layered section's group is about, where its own file
 * is that one — and **nothing** where it is not.
 *
 * A section that came from the desk-level file is not one This project speaks
 * for, and a row that dropped its status under that title would be attributing
 * a value to a file it did not come from. So that section is given no group at
 * all and states its own status, exactly as it did before there were groups.
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
