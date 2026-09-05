/**
 * The mode, in the address, as a search parameter.
 *
 * `?edit` and not `/packs/:id/edit`, and the reason is the dirty blocker:
 * `useDirtyGuard`'s predicate is `currentLocation.pathname !==
 * nextLocation.pathname`, so a mode written into the path would ask "leave
 * without saving?" every single time a viewer switched to Read — and a
 * predicate loose enough to allow it would stop asking on the exits it exists
 * for. As a search parameter the toggle is the same page: same mount, same
 * scroll, same selection, same buffer.
 *
 * Writes are `replace: true`, for the reason `?at` writes are: choosing how to
 * look at a document is not a navigation and must not fill the Back stack.
 * They carry the rest of the address through, so switching to Read does not
 * drop the member being inspected.
 */
import type { URLSearchParamsInit } from 'react-router-dom'

/** Which shape the document is edited in. */
export type EditShape = 'form' | 'json'

export const EDIT_PARAM = 'edit'
export const SHAPE_PARAM = 'shape'

/** Whether the address asks for edit mode. */
export function isEditing(params: URLSearchParams): boolean {
  // Presence is the question, exactly as it is for the evidence document on
  // the wire: `?edit`, `?edit=1` and `?edit=` all say the same thing, and
  // absence is the only form of no.
  return params.get(EDIT_PARAM) !== null
}

/** Which shape the address asks for; form unless it says otherwise. */
export function editShape(params: URLSearchParams): EditShape {
  return params.get(SHAPE_PARAM) === 'json' ? 'json' : 'form'
}

/** The address with edit mode turned on or off, everything else kept. */
export function withEditing(params: URLSearchParams, editing: boolean): URLSearchParamsInit {
  const next = new URLSearchParams(params)
  if (editing) next.set(EDIT_PARAM, '1')
  else {
    next.delete(EDIT_PARAM)
    // The shape is a fact about editing, so it leaves with it rather than
    // sitting in the address of a page that is not being edited.
    next.delete(SHAPE_PARAM)
  }
  return next
}

/** The address with the shape set, everything else kept. */
export function withShape(params: URLSearchParams, shape: EditShape): URLSearchParamsInit {
  const next = new URLSearchParams(params)
  if (shape === 'json') next.set(SHAPE_PARAM, 'json')
  else next.delete(SHAPE_PARAM)
  return next
}
