import { msg } from '../i18n'
/**
 * Settings navigation. Groups follow tasks; each section states its own scope
 * (this desk, or shared on this computer).
 */
export interface AdminSection {
  id: string
  title: string
}

export interface AdminGroup extends AdminSection {
  /** The members of this group, in the order the page renders them. */
  sections: readonly AdminSection[]
}

export const ADMIN_GROUPS: readonly AdminGroup[] = [
  {
    id: 'workspace',
    get title() { return msg('Workspace') },
    sections: [
      { id: 'general', get title() { return msg('General') } },
      { id: 'assistant', get title() { return msg('Assistant') } },
      { id: 'research', get title() { return msg('Research') } },
      { id: 'storage', get title() { return msg('Storage & backups') } },
      { id: 'safeguards', get title() { return msg('Decision safeguards') } }
    ]
  },
  {
    id: 'services',
    get title() { return msg('Connections & access') },
    sections: [
      { id: 'connections', get title() { return msg('Connections') } },
      { id: 'gateway', get title() { return msg('Document processing') } },
      { id: 'identity-provider', get title() { return msg('Sign-in & access') } }
    ]
  }
]

/**
 * Every section, in page order, flattened out of the groups above.
 *
 * Derived rather than declared a second time: the rail's section menu and this
 * page would otherwise be two lists free to disagree about what Admin has on
 * it, which is exactly what a link to a section that is no longer there is.
 */
export const ADMIN_SECTIONS: readonly AdminSection[] = ADMIN_GROUPS.flatMap(
  (group) => group.sections
)

/**
 * The fragments this page answered to before its sections were grouped by
 * task, and the Connections tabs, each resolved to the section that now holds
 * what it opened.
 */
const ALIASES: Record<string, string> = {
  project: 'general',
  organization: 'general',
  documents: 'gateway',
  'connections-ai': 'connections',
  'connections-files': 'connections',
  'connections-search': 'connections'
}

/**
 * The section a fragment names, and the **first** section for every fragment
 * that names none: no fragment, a fragment naming no section — a group id, a
 * section that was renamed, a link somebody typed — and one that is not valid
 * percent-encoding. There is no state in which nothing is open.
 */
export function adminSectionId(hash: string): string {
  const first = ADMIN_SECTIONS[0]!.id
  let id: string
  try {
    id = decodeURIComponent(hash.replace(/^#/, ''))
  } catch {
    return first
  }
  return ALIASES[id] ?? (ADMIN_SECTIONS.some((section) => section.id === id) ? id : first)
}

/** The Connections tab a fragment opens: `#connections-ai`, `#connections-search`, or Files & apps. */
export function connectionTab(hash: string): 'ai' | 'files' | 'search' {
  if (hash === '#connections-ai') return 'ai'
  if (hash === '#connections-search') return 'search'
  return 'files'
}
