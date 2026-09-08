/**
 * The configured theme and density, written onto the root element.
 *
 * `appearance.theme` is the one configuration key whose effect is a selector
 * rather than a string on a page, so it is applied in one place and nowhere
 * else: `light` and `dark` pin a palette by attribute, `system` removes the
 * attribute and leaves `prefers-color-scheme` to answer. `styles.css` carries
 * the two blocks that read it.
 *
 * **Both palettes are real.** `styles.css` carries the dark values in both of
 * the blocks that select dark, every colour token has one, and every pair a
 * reader has to see is measured at WCAG AA in each — so choosing dark now
 * changes the colours and not only the attribute.
 *
 * **There is still no pre-paint inline script, and one cannot be written for
 * the preference.** Under `system` — the default — nothing flashes: the media
 * block paints the dark palette on the first paint, before any script runs. An
 * explicit `dark` over an OS set to light is the case a pre-paint script would
 * be for, and the record that holds it is keyed on the chassis' project root,
 * which the page does not know until the file listing answers. A script that
 * guessed the key would apply one project's preference to another's desk. So
 * that one case paints light for a frame, and the README says so rather than
 * this file pretending otherwise.
 */
import { useEffect } from 'react'
import type { Density, ThemeChoice } from './deskConfig'

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
 * two of them wrong, and — now that both palettes are real — a visible flash. An
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

export const DENSITY_ATTRIBUTE = 'data-density'

/**
 * Write one density choice onto `<html>`, or clear it for `comfortable`.
 *
 * The theme's shape exactly, and for the theme's reason: the effect of this
 * member is a selector rather than a string on a page, so it is applied in one
 * place and read in one place — `:root[data-density="compact"]` in
 * `styles.css`, which tightens the six numbers of the spacing scale.
 *
 * **`comfortable` removes the attribute rather than writing it.** The
 * comfortable scale is the one on bare `:root`, so there is nothing for a
 * `data-density="comfortable"` selector to say; writing one would be a second
 * spelling of the default that every future rule would have to remember to
 * match. It is not the theme's `system`, though it clears the attribute the
 * same way: `system` defers to the OS and there is no OS density to defer to.
 */
export function applyDensity(density: Density): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  if (density === 'comfortable') {
    root.removeAttribute(DENSITY_ATTRIBUTE)
    return
  }
  root.setAttribute(DENSITY_ATTRIBUTE, density)
}

/**
 * Keep the attribute in step with the density once there is one, and take it
 * back off on unmount.
 *
 * `useAppliedTheme`'s rules, for `useAppliedTheme`'s reasons: `undefined` is
 * "not known yet" and writes nothing, because the viewer's preference needs a
 * project root the chassis has not reported and the project's default needs a
 * file that has not been read; and a desk that has left the page should not
 * still be sizing it.
 */
export function useAppliedDensity(density: Density | undefined): void {
  useEffect(() => {
    if (density === undefined) return
    applyDensity(density)
    return () => applyDensity('comfortable')
  }, [density])
}

/**
 * One list row's height in pixels, per density.
 *
 * **The one number the sheet cannot keep to itself.** The packs list is
 * windowed: it renders the rows on screen and reserves the rest as two
 * spacers, and that arithmetic is done in JavaScript against a row height. A
 * `--density-row` the sheet tightened while this stayed at 40 would leave the
 * list scrolling to the wrong place and focusing the wrong row.
 *
 * So it is written down once here and `ui/palette.test.ts` reads
 * `--density-row` out of both blocks of `styles.css` and holds these two
 * numbers equal to them. A duplicated constant that a test ties to its source
 * is not two answers; a duplicated constant that nothing ties is.
 */
export const ROW_HEIGHT: Record<Density, number> = {
  comfortable: 40,
  compact: 32
}
