/**
 * Admin's sections, as a typed array.
 *
 * **What is not here is the point.** No users, no roles, no invitations, no
 * assignment, no review queues, no audit-of-the-review-process. Each of those
 * needs an account model this desk does not have and will not grow one to
 * satisfy a menu: users, roles and invitations belong to whatever identity
 * provider a deployment configures, and hiding Admin from some viewers would
 * claim a role model the desk has never defined. The list is an array rather
 * than a set of headings scattered through a component, so that adding one is
 * a reviewable line rather than a paragraph someone slipped in.
 *
 * Every section is about **this machine's desk**. Every one of them is
 * read-only except Assistant, which carries the desk's one write control: a
 * key must never be pasted into a project file, so it cannot be written the
 * way every other setting is.
 */
export interface AdminSection {
  id: string
  title: string
}

export const ADMIN_SECTIONS: readonly AdminSection[] = [
  { id: 'organization', title: 'Organization' },
  { id: 'identity-provider', title: 'Identity provider' },
  // After Identity provider, because it is the other desk-level slot and is
  // built on the same one-nullable-field pattern.
  { id: 'assistant', title: 'Assistant' },
  { id: 'runtime', title: 'Runtime' },
  { id: 'project', title: 'Project' },
  // After Project, because it is about where this project's packs live.
  { id: 'storage', title: 'Storage' },
  { id: 'appearance', title: 'Appearance' },
  { id: 'panes', title: 'Panes' }
]

/** The sentence the page carries, verbatim, above everything else. */
export const ADMIN_DISCLAIMER =
  'This desk defines no accounts and no roles. Configuring a provider changes what the header ' +
  'displays and nothing about who may reach the desk — access is the loopback bind, the ' +
  'session token this tab holds, and the origin check.'
