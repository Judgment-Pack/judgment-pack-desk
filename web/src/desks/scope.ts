// Freeze the desk for this document: an in-flight save can never change desks.
const KEY = 'jpack.active-desk'
// Missing means a fresh visit; an explicit empty string selects the owner.
export function readDeskSelection(): string | undefined {
  const id = sessionStorage.getItem(KEY)
  if (id === null) return undefined
  if (id !== '' && !/^[a-f0-9]{32}$/.test(id)) throw Error('Invalid desk')
  return id
}
export function selectDesk(id: string): void {
  if (id !== '' && !/^[a-f0-9]{32}$/.test(id)) throw Error('Invalid desk')
  sessionStorage.setItem(KEY, id)
}
export const activeDeskId = (() => {
  try { const id = sessionStorage.getItem(KEY); return id && /^[a-f0-9]{32}$/.test(id) ? id : '' } catch { return '' }
})()
export function deskHeaders(): Record<string, string> { return activeDeskId ? {'X-Jpack-Desk': activeDeskId} : {} }
export function deskSocketURL(url: string): string { return activeDeskId ? `${url}${url.includes('?') ? '&' : '?'}desk=${activeDeskId}` : url }
export function openDesk(id: string) {
  if (id && !/^[a-f0-9]{32}$/.test(id)) throw Error('Invalid desk')
  selectDesk(id)
  window.location.assign('/')
}

export function deskEventPath(trigger: string): string {
  return activeDeskId ? `/api/desks/${activeDeskId}/job-events/${trigger}` : `/api/job-events/${trigger}`
}
