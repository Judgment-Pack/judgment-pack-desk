/**
 * Admin's groups and their sections, as typed arrays — and the order the page
 * renders them in.
 *
 * **The grouping is by file, and that is the page's argument.** Every setting
 * on Admin is a member of one of two files, and which file it is in is the
 * thing a reader most needs to know: it is where the value is written, what the
 * `Status` line is about, and whether this desk can write it at all. Grouping
 * by file lets that be said **once**, in the group's header, instead of three
 * times over three cards that turned out to be three members of one document.
 *
 * **The order is the page's argument too.** This project comes first because it
 * is the one an admin is here to point at, and this desk second because it is
 * the machine's answer for every project it opens. It is an array rather than a
 * set of headings scattered through a component, so that adding one is a
 * reviewable line rather than a paragraph someone slipped in.
 *
 * **The first section is the landing page**, because Admin has no overview:
 * `/admin`, an unknown fragment and a malformed one all open it. So the order
 * decides what a reader sees on arriving, and Project is what they see.
 *
 * **Runtime, Panes and Appearance are not here, and their absence is the
 * change.** None was a setting. The Runtime card reported the binary the
 * chassis was launched with and whether the socket is up — status, which now
 * has a line of its own and a home in Help & About. The Panes card offered
 * three pane dimensions and a reset of this browser's own record of the layout:
 * the dimensions are the shell's, and the reset moved to the shell's own menu,
 * where the panes are. Appearance was a *person's* theme and density written
 * into the project's shared file, so one viewer's dark was everybody's; it is
 * the user menu's now. Both members are still in the schema, still decoded and
 * still applied — `appearance` as the default for whoever has not chosen.
 *
 * **What is not here is still the point.** No users, no roles, no invitations,
 * no assignment, no review queues. Each of those needs an account model this
 * desk does not have and will not grow one to satisfy a menu: users, roles and
 * invitations belong to whatever identity provider a deployment configures, and
 * hiding Admin from some viewers would claim a role model the desk has never
 * defined.
 */
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
    title: 'This project',
    sections: [
      // **First, and it is the file rather than a member of it.** Admin has no
      // overview any more, so the two rows the group header used to carry —
      // where this project's file is, and what reading it produced — have
      // nowhere else to be said in the main column. They are a section, with
      // the one control that is about the *project* rather than about a
      // member: whether this desk opens it when it is launched with no
      // directory.
      { id: 'project', title: 'Project' },
      { id: 'organization', title: 'Organization' },
      // After Organization, because it is about where this project's packs live.
      { id: 'storage', title: 'Storage' }
    ]
  },
  {
    id: 'this-desk',
    title: 'This desk',
    sections: [
      { id: 'assistant', title: 'Assistant' },
      // After Assistant, because it is the other desk-level slot and is built
      // on the same one-nullable-field pattern.
      { id: 'identity-provider', title: 'Identity provider' }
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
