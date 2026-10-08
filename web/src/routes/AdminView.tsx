/**
 * Admin: settings navigation grouped by task, one open section, and the file
 * itself in the right pane.
 *
 * **Sections follow tasks, and each says its own scope.** Workspace holds what
 * a person sets up for this desk — General (the desk's name, branding and the
 * startup project), Assistant (this desk's model preferences), Research (this
 * desk's web research policy), Storage & backups, and Decision safeguards (the
 * gates, the decision record and the Jobs record). Connections & access holds
 * what is shared on this computer: Connections, as tabs (AI, Files & apps, Web
 * search), Document processing, and Sign-in & access. Each section states
 * whether its changes apply to this desk or are shared on this computer.
 *
 * **A row is a link and the hash is the state.** `/admin#assistant` opens the
 * assistant section. A fragment that names no section opens the first one, as
 * does no fragment and one that is not valid percent-encoding; the fragments
 * of the sections this page had before (`#project`, `#organization`,
 * `#documents`) and the Connections tabs (`#connections-ai`,
 * `#connections-search`) resolve in `adminSectionId`.
 *
 * **The bytes are in the right pane.** They are context and not a setting.
 * Details › Configuration file opens the file the open section is about —
 * the whole project file under General, one member of whichever file supplied
 * it elsewhere — and Details › Runtime details what this desk is running on.
 * Nothing about what may be shown changed: a refused file's bytes are still
 * never rendered, and the gate is the card's own `showsContent`, applied by
 * `ConfigPane`.
 *
 * **A location is never composed here, and never stood in for.** The
 * desk-level file's path, the project file's path and the runtime binary come
 * from the chassis; where the chassis has not answered, the row says so.
 *
 * The shell hosts the section links in its settings sidebar; standalone
 * renders keep an inline navigation column. Every section is a
 * `RetainedPanel`, so a draft survives a visit to another section, and each
 * section keeps its own Save.
 *
 * `runtime` and the project root are **not in the schema**, and that is the
 * design rather than a gap: `relay.go` runs the configured binary, so a
 * config-supplied path would be a local-code-execution surface. The runtime
 * details report what the process was started with.
 */
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { DropdownMenu } from 'radix-ui'
import { msg, useLocale } from '../i18n'
import { DESK_LEVEL_PATH_UNKNOWN } from '../config/queries'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import type { ConfigProblem, DeskConfig, EffectiveConfig, ValueSource } from '../config/deskConfig'
import { useFileListing } from '../files/queries'
import { desksAPI } from '../desks/DeskSwitcher'
import { ConnectionSettings } from '../admin/ConnectionSettings'
import { DocumentProcessingSettings } from '../admin/DocumentProcessingSettings'
import { ChatDataSettings } from '../admin/ChatDataSettings'
import { AssistantSection } from '../assistant/AssistantSection'
import { SharedAssistantSettings } from '../assistant/AssistantSettings'
import { AdminStatusLine } from '../admin/AdminStatusLine'
import { ConfigPane } from '../admin/ConfigPane'
import { SourceCard, CardField, type SourceStatus } from '../admin/SourceCard'
import { useDefaultProject } from '../admin/DefaultProject'
import { OrganizationForm, StorageForm, StorageKind } from '../admin/projectFileCards'
import { SignInSettings } from '../auth/SignInSettings'
import { DecisionRecord } from '../audit/DecisionRecord'
import { JobsRecord } from '../audit/JobsRecord'
import { ProjectGates } from '../packs/upgrade/UpgradeNote'
import { SearchConnectionsSettings, WebSearchSettings } from '../search/WebSearchSettings'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { Button, ButtonLink } from '../ui/Button'
import { SettingsSection, SettingsStack } from '../ui/SettingsSection'
import { Tabs } from '../ui/Tabs'
import { RetainedPanel } from '../ui/RetainedPanel'
import { DraftScope } from '../shell/DraftScope'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { useSettingsNavigation } from '../shell/SettingsNavigation'
import { connectionSays, useMcp } from '../mcp/McpProvider'
import { ADMIN_GROUPS, ADMIN_SECTIONS, adminSectionId, connectionTab } from './adminSections'
import styles from './AdminView.module.css'

export function AdminView() {
  useLocale()
  const navigation = useSettingsNavigation()
  const effective = useEffectiveConfig()
  const mcp = useMcp()
  const listing = useFileListing()
  const { hash } = useLocation()
  const navigate = useNavigate()
  const id = adminSectionId(hash)
  const open = ADMIN_SECTIONS.find((section) => section.id === id)!
  const tab = connectionTab(hash)
  // General's Startup field and its Save, sharing one draft across two slots.
  const defaultProject = useDefaultProject()
  const directory = useQuery({ queryKey: ['desks'], queryFn: () => desksAPI<{ current: { name: string } }>() })
  // **One unsaved draft at a time between the desk's model preferences and the
  // shared AI connections.** Each blocks the other while it holds an edit, so
  // a save on one never lands on a base the other has moved.
  const [profileDirty, setProfileDirty] = useState(false)
  const [sharedDirty, setSharedDirty] = useState(false)
  const [inspection, setInspection] = useState<'config' | 'runtime'>('config')
  const details = useDetailsSlot()
  // **Each section starts at the top of the page.** A full load of
  // `/admin#assistant` is scrolled by the browser itself — the shell's scroll
  // container is `.desk-main` and the browser scrolls the nearest one — and a
  // click on a row while a tall section is scrolled would otherwise open the
  // next one halfway down. Stated on this page's own element rather than on
  // the shell's, so a route is not selecting the frame it is rendered in.
  const top = useRef<HTMLElement>(null)
  useEffect(() => {
    top.current?.scrollIntoView()
    details.dismissInspection?.()
  }, [id, details.dismissInspection])

  // **Which member of which file the pane is about**, where the open section
  // is about one: General is the whole project file; Storage & backups, the
  // project's `storage`; Document processing, the desk-level `research`; and
  // Connections › AI, the desk-level `assistant`.
  const member = id === 'storage' ? 'storage'
    : id === 'gateway' ? 'research'
    : id === 'connections' && tab === 'ai' ? 'assistant'
    : undefined
  const canInspect = id === 'general' || member !== undefined
  const configPane = canInspect ? <ConfigPane {...paneFor(effective, member)} /> : null
  // **The pane, claimed for as long as this route is mounted.** The claim and
  // the portal are one call, so leaving Admin releases the slot and the next
  // route's own panel — or the pane's empty state — takes it back.
  const pane = useDetailsPortal(inspection === 'runtime'
    ? <SettingsSection title={msg('Runtime details')} variant="plain">
      <AdminStatusLine runtime={runtimeSays(mcp)} binary={runtimeBinary(effective)}
        copyText={effective.desk?.chassis === undefined ? undefined : `${runtimeSays(mcp)}\n${effective.desk.chassis.runtimeBin}`} />
    </SettingsSection>
    : configPane)
  const inspect = (kind: 'config' | 'runtime') => {
    setInspection(kind)
    details.reveal()
  }
  const sectionId = (value: string) => navigation.inSidebar ? `settings-${value}` : value

  return (
    <article ref={top} className={`detail ${styles.admin}`} id={navigation.inSidebar ? open.id : undefined} data-measure="full" data-layout="page" data-navigation={navigation.inSidebar ? 'sidebar' : 'inline'}>
      {pane}
      <PageHeader title={msg('Admin')} context={open.title} actions={<DropdownMenu.Root>
        <DropdownMenu.Trigger asChild><Button variant="quiet">{msg('Details')}</Button></DropdownMenu.Trigger>
        <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={4}>
          {canInspect && <DropdownMenu.Item className="desk-menu-item" onSelect={() => inspect('config')}>{msg('Configuration file')}</DropdownMenu.Item>}
          <DropdownMenu.Item className="desk-menu-item" onSelect={() => inspect('runtime')}>{msg('Runtime details')}</DropdownMenu.Item>
          <DropdownMenu.Item className="desk-menu-item" asChild><Link to="/author">{msg('Project files')}</Link></DropdownMenu.Item>
        </DropdownMenu.Content></DropdownMenu.Portal>
      </DropdownMenu.Root>} />
      <DraftScope>
        <PageBody width={navigation.inSidebar ? 'form' : 'wide'}>
          <div className={styles.split}>
            {navigation.render(<nav className={styles.rail} data-sidebar={navigation.inSidebar || undefined} aria-label={msg('Settings')}>
              {ADMIN_GROUPS.map((group) => (
                <div className={styles.railGroup} key={group.id}>
                  <p className={styles.railTitle} id={`rail-${group.id}`}>{group.title}</p>
                  <ul className={styles.rows} aria-labelledby={`rail-${group.id}`}>
                    {group.sections.map((section) => (
                      <li key={section.id} className={styles.rowItem}>
                        <Link className={styles.row} to={`/admin#${section.id}`} aria-current={id === section.id ? 'page' : undefined}>
                          <span className={styles.rowTitle}>{section.title}</span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </nav>)}
            <div className={styles.open}>
              <RetainedPanel active={id === 'general'}>
                <SettingsSection title={msg('General')} level={2} variant="standalone" description={msg('Desk identity, branding and startup preferences.')}>
                  <SettingsStack>
                    <CardField label={msg('Desk name')}>
                      {directory.data?.current.name ?? (directory.isError ? msg('could not be read') : msg('Loading…'))}
                    </CardField>
                    <SourceCard id={sectionId('branding')} title={msg('Branding')} level={3} presentation="settings"
                      location={sectionLocation(effective, 'organization')} status={sectionStatus(effective, 'organization')}
                      description={effective.sources.organization === 'desk file' ? msg('Shared on this computer') : msg('This desk')}
                      save={<OrganizationForm />} />
                    <SettingsSection title={msg('Startup')} variant="plain" description={msg('Shared on this computer · Applies when Desk starts without a project folder.')}>
                      {defaultProject.field}
                      {defaultProject.save}
                    </SettingsSection>
                  </SettingsStack>
                </SettingsSection>
              </RetainedPanel>
              <RetainedPanel active={id === 'assistant'}>
                {sharedDirty && <p role="status" className={styles.explanation}>{msg('Save or discard changes in Connections before editing desk preferences.')}</p>}
                <AssistantSection id={sectionId('assistant')} title={msg('Assistant')} level={2} under={deskStatus(effective)} blocked={sharedDirty} onDirtyChange={setProfileDirty} />
              </RetainedPanel>
              <RetainedPanel active={id === 'research'}><WebSearchSettings /></RetainedPanel>
              <RetainedPanel active={id === 'storage'}>
                <SettingsSection title={msg('Storage & backups')} level={2} variant="standalone" description={msg('Manage pack files, saved conversations and chat backups.')}>
                  <SettingsStack>
                    <SourceCard id={sectionId('pack-storage')} title={msg('Pack files')} level={3} presentation="settings"
                      location={sectionLocation(effective, 'storage')} status={sectionStatus(effective, 'storage')}
                      description={effective.sources.storage === 'desk file' ? msg('Shared on this computer') : msg('This desk')}
                      fields={<StorageKind />}
                      save={<StorageForm dirSays={PACK_LOCATION_SAYS[packLocationState(effective.config.storage.packs.dir, listing)]} />} />
                    <ChatDataSettings />
                  </SettingsStack>
                </SettingsSection>
              </RetainedPanel>
              {/* The upgrade offer stays available here (ADR-0009, section 4);
                  beside the gates, what the runtime finds in the trail they keep
                  (ADR-0010, section 4), with the hand-over, repair and stamping
                  it carries; and beside it, where this desk has a Runner, what
                  the runtime finds in a copy of Runner's chain of runs (ADR-0010,
                  section 4, "A Jobs record panel"). Each runs only while this
                  section is open. */}
              <RetainedPanel active={id === 'safeguards'}>
                <SettingsStack>
                  <ProjectGates />
                  <DecisionRecord visible={id === 'safeguards'} />
                  <JobsRecord visible={id === 'safeguards'} />
                </SettingsStack>
              </RetainedPanel>
              <RetainedPanel active={id === 'connections'}>
                <SettingsSection title={msg('Connections')} level={2} variant="standalone" description={msg('Shared on this computer · Connect AI, file sources and web search.')}>
                  <Tabs variant="settings" activationMode="manual" label={msg('Connection types')} scrollable value={tab}
                    onValueChange={(value) => navigate(`/admin#connections${value === 'files' ? '' : `-${value}`}`)} keepMounted tabs={[
                      // AI connections are shared on this computer
                      // (docs/ai-connections.md): kept mounted, like the
                      // Assistant section, so an unsaved edit survives a visit
                      // to another tab or section.
                      { value: 'ai', label: msg('AI'), panel: <RetainedPanel active={id === 'connections' && tab === 'ai'}>
                        <div className={styles.connectionSettings}>
                          <p className={styles.explanation}>{msg('Connections are shared on this computer. Each desk chooses which connections and models to use.')}</p>
                          {profileDirty && <p role="status" className={styles.explanation}>{msg('Save or discard changes in Assistant before editing shared AI settings.')}</p>}
                          <SharedAssistantSettings unavailable={profileDirty} onDirtyChange={setSharedDirty} />
                          <ButtonLink variant="quiet" to="/admin#assistant">{msg('Model preferences for this desk')}</ButtonLink>
                        </div>
                      </RetainedPanel> },
                      { value: 'files', label: msg('Files & apps'), panel: id === 'connections' && tab === 'files' ? <ConnectionSettings embedded /> : null },
                      { value: 'search', label: msg('Web search'), panel: id === 'connections' && tab === 'search' ? <SearchConnectionsSettings /> : null }
                    ]} />
                </SettingsSection>
              </RetainedPanel>
              <RetainedPanel active={id === 'gateway'}><DocumentProcessingSettings /></RetainedPanel>
              {id === 'identity-provider' && <SignInSettings />}
            </div>
          </div>
        </PageBody>
      </DraftScope>
    </article>
  )
}

/**
 * What the right pane is about: the whole project file under General, and one
 * member of whichever file supplied it under every other section that has one.
 *
 * The title names the file by the name this desk knows it by — the project's
 * own is the name it is read at, and the desk-level file's is the last segment
 * of the path the chassis reported. Where the chassis has named no file, the
 * title is the member alone rather than a file name this page made up.
 */
function paneFor(
  effective: EffectiveConfig,
  member?: 'storage' | 'research' | 'assistant'
): {
  title: string
  location: ReactNode
  status: SourceStatus
  digest?: string
  text?: string
  member?: string
} {
  if (member === undefined) {
    return {
      title: effective.path,
      location: projectLocation(effective),
      status: projectStatus(effective),
      digest: effective.sha256,
      text: effective.text
    }
  }
  // `assistant` and `research` are only ever the desk-level file's.
  const fromDesk = member === 'assistant' || member === 'research' || effective.sources[member] === 'desk file'
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
  if (chassis === undefined) return <span className="quiet">{msg("the desk has not said")}</span>
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
  if (chassis === undefined) return <span className="quiet">{msg("the desk has not said")}</span>
  return <code>{chassis.runtimeBin}</code>
}

/** Where the desk-level file is, as the chassis said it — or that nothing asked. */
function deskLocation(effective: EffectiveConfig) {
  if (effective.desk === undefined) {
    return <span className="quiet">{msg("nothing has asked for it")}</span>
  }
  return effective.desk.path === DESK_LEVEL_PATH_UNKNOWN
    ? <span className="quiet">{msg("the chassis did not say where")}</span>
    : <code>{effective.desk.path}</code>
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
  get pending() { return msg("the file listing has not answered yet") },
  get failed() { return msg("the file listing failed, so nothing is known about it") },
  get partial() { return msg("the file listing came back incomplete, so nothing is known about it") },
  get obstructed() { return msg("a file is there under that exact name — nothing can be created inside it") },
  get 'holds-files'() { return msg("holds files") },
  get 'no-file-under-it'() { return msg("no file is under it — the first pack asks for it to be created") }
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
