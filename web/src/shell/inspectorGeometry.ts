/** CSS-pixel preferences; viewport clamps never overwrite a viewer's choice. */
export const INSPECTOR_MIN = 320
export const INSPECTOR_MAX = 640
export const MAIN_MIN = 480

export function inspectorGeometry(workspaceWidth: number | undefined, preferred: number, requiredMain: number, narrow: boolean) {
  const available = Math.max(0, (workspaceWidth ?? 0) - 2)
  const floor = Math.max(MAIN_MIN, requiredMain)
  const capacity = workspaceWidth ? Math.floor(Math.min(INSPECTOR_MAX, available * .45, available - floor)) : INSPECTOR_MAX
  const drawer = narrow || capacity < INSPECTOR_MIN
  const max = drawer ? INSPECTOR_MAX : capacity
  const width = Math.round(Math.min(max, Math.max(INSPECTOR_MIN, preferred)))
  return { drawer, width, min: INSPECTOR_MIN, max }
}
