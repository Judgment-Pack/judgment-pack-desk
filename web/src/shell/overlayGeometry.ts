/** Expanded widths belong to the viewer; viewport clamps never rewrite them. */
export const OVERLAY_MIN = 640
export const PACK_PEEK = 240
export const validOverlayWidth = (value: unknown): value is number =>
  typeof value === 'number' && Number.isInteger(value) && value >= OVERLAY_MIN && value <= 16384

/** Available width excludes navigation, the tool rail and workspace borders. */
export function overlayGeometry(available: number, preferred?: number, leadingWidth = 0) {
  const room = Number.isFinite(available) ? Math.max(0, Math.floor(available)) : 0
  const peek = PACK_PEEK + (Number.isFinite(leadingWidth) ? Math.max(0, leadingWidth) : 0)
  const full = room < OVERLAY_MIN + peek
  const max = full ? room : room - peek
  const min = full ? room : OVERLAY_MIN
  const initial = Math.round(room * .75)
  const width = full ? room : Math.min(max, Math.max(min, validOverlayWidth(preferred) ? preferred : initial))
  return { full, min, max, width }
}
