/**
 * Admin's sections, as a typed array — and the order the page renders them in.
 *
 * **The order is the page's argument.** The project file comes first because
 * it is the one an admin is here to point at; the identity provider is next
 * because it is the other thing a deployment configures; and the rest follow in
 * the order they are asked about. It is an array rather than a set of headings
 * scattered through a component, so that adding one is a reviewable line rather
 * than a paragraph someone slipped in.
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

export const ADMIN_SECTIONS: readonly AdminSection[] = [
  { id: 'project', title: 'Project file' },
  { id: 'identity-provider', title: 'Identity provider' },
  // After Identity provider, because it is the other desk-level slot and is
  // built on the same one-nullable-field pattern.
  { id: 'assistant', title: 'Assistant' },
  { id: 'runtime', title: 'Runtime' },
  // After Runtime, because it is about where this project's packs live.
  { id: 'storage', title: 'Storage' },
  { id: 'organization', title: 'Organization' },
  { id: 'appearance', title: 'Appearance' },
  { id: 'panes', title: 'Panes' }
]
