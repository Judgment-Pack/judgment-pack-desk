import { msg } from '../i18n'
/** Settings navigation. Storage & data combines project file settings with
 * explicitly personal chat storage; each section states its own scope. */
export interface AdminSection {
  id: string
  title: string
}

export interface AdminGroup extends AdminSection {
  /** The members of this group's file, in the order the page renders them. */
  sections: readonly AdminSection[]
}

export const ADMIN_GROUPS: readonly AdminGroup[] = [
  {
    id: 'this-project',
    get title() { return msg("This project") },
    sections: [
      // **First, and it is the file rather than a member of it.** Admin has no
      // overview any more, so the two rows the group header used to carry —
      // where this project's file is, and what reading it produced — have
      // nowhere else to be said in the main column. They are a section, with
      // the one control that is about the *project* rather than about a
      // member: whether this desk opens it when it is launched with no
      // directory.
      { id: 'project', get title() { return msg("Project") } },
      { id: 'organization', get title() { return msg("Organization") } },
      // After Organization, because it is about where this project's packs live.
      { id: 'storage', get title() { return msg("Storage & data") } }
    ]
  },
  {
    id: 'this-desk',
    get title() { return msg("This desk") },
    sections: [
      { id: 'assistant', get title() { return msg("Assistant") } },
      { id: 'connections', get title() { return msg("Connections") } },
      { id: 'identity-provider', get title() { return msg('Sign-in & access') } }
    ]
  }
]

/**
 * Every card, in page order, flattened out of the groups above.
 *
 * Derived rather than declared a second time: the rail's section menu and this
 * page would otherwise be two lists free to disagree about what Admin has on
 * it, which is exactly what a link to a section that is no longer there is.
 */
export const ADMIN_SECTIONS: readonly AdminSection[] = ADMIN_GROUPS.flatMap(
  (group) => group.sections
)
