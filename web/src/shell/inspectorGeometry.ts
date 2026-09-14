/** CSS-pixel preferences; viewport clamps never overwrite a viewer's choice. */
export const INSPECTOR_MIN = 320
export const INSPECTOR_MAX = 640
export const MAIN_MIN = 480

export function inspectorGeometry(workspaceWidth: number | undefined, preferred: number, requiredMain: number, narrow: boolean, maximum = INSPECTOR_MAX) {
  const available = Math.max(0, (workspaceWidth ?? 0) - 2)
  const floor = Math.max(MAIN_MIN, requiredMain)
  const limit = Math.max(INSPECTOR_MIN, Math.min(INSPECTOR_MAX, maximum))
  const capacity = workspaceWidth ? Math.floor(Math.min(limit, available * .45, available - floor)) : limit
  const drawer = narrow || capacity < INSPECTOR_MIN
  const max = drawer ? limit : capacity
  const width = Math.round(Math.min(max, Math.max(INSPECTOR_MIN, preferred)))
  return { drawer, width, min: INSPECTOR_MIN, max }
}
