/**
 * The configured theme, written onto the root element.
 *
 * `appearance.theme` is the one configuration key whose effect is a selector
 * rather than a string on a page, so it is applied in one place and nowhere
 * else: `light` and `dark` pin a palette by attribute, `system` removes the
 * attribute and leaves `prefers-color-scheme` to answer. `styles.css` carries
 * the two blocks that read it.
 *
 * **What this does today is set an attribute, and that is the whole of it.**
 * Both palette blocks carry the light values, because the three condition
 * verdict colours cannot be mechanically inverted and a desk that re-authored
 * its neutrals around them would be half dark (open question 10). So choosing
 * dark changes the attribute and no colour. Admin and the README say exactly
 * that, and this comment is the third place it is written down rather than
 * inferred.
 *
 * **There is no pre-paint inline script**, deliberately. One belongs with the
 * dark palette itself — its whole purpose is to stop a light frame flashing
 * before the dark one arrives, and there is no dark frame yet to flash into.
 * Adding it now would be a script guarding against nothing, and it would have
 * to read the configuration from somewhere the page has not yet fetched.
 */
import { useEffect } from 'react'
import type { ThemeChoice } from './deskConfig'

export const THEME_ATTRIBUTE = 'data-theme'

/** Write one theme choice onto `<html>`, or clear it for `system`. */
export function applyTheme(theme: ThemeChoice): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  if (theme === 'system') {
    root.removeAttribute(THEME_ATTRIBUTE)
    return
  }
  root.setAttribute(THEME_ATTRIBUTE, theme)
}

/**
 * Keep the attribute in step with the theme once there **is** one, and take it
 * back off on unmount — a desk that is not on the page should not still be
 * theming it.
 *
 * `undefined` is "not known yet", and it writes nothing and removes nothing.
 * The desk's theme is the viewer's preference over the project file's default,
 * and neither is readable at first paint: the record needs the root the chassis
 * has not reported yet, and the default needs a file that has not been read. A
 * caller that painted the schema default while it waited would apply `system`,
 * then the file's value, then the stored one — three applications for one load,
 * two of them wrong, and a visible flash the day a dark palette exists. An
 * attribute this desk has not yet decided about is also not this desk's to
 * clear, so nothing is removed either.
 */
export function useAppliedTheme(theme: ThemeChoice | undefined): void {
  useEffect(() => {
    if (theme === undefined) return
    applyTheme(theme)
    return () => applyTheme('system')
  }, [theme])
}
