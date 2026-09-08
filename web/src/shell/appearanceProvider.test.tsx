/**
 * The provider's lifetime: which project a choice belongs to, and when this
 * desk knows enough to paint one.
 *
 * The store's own rules are proved without React in `appearanceState.test.ts`.
 * What needs a render is what happens *between* mount and the chassis' answer
 * about which project this is, because that answer arrives after the first
 * paint and what it changes is which record a choice belongs to.
 */
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { DESK_DEFAULTS, type AppearanceConfig } from '../config/deskConfig'
import { AppearanceProvider, appearanceKey, useAppearance } from './appearanceState'
import { projectKey } from './paneState'

afterEach(() => {
  cleanup()
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

const A = '/home/someone/project-a'
const B = '/home/someone/project-b'
const KEY_A = appearanceKey(projectKey(A))
const KEY_B = appearanceKey(projectKey(B))

const LIGHT_COMPACT: AppearanceConfig = { theme: 'light', density: 'compact' }

/** What the menu would show, and one control to drive it with. */
function Probe() {
  const appearance = useAppearance()
  return (
    <>
      <p data-testid="effective">{`${appearance.theme}/${appearance.density}`}</p>
      <button type="button" onClick={() => appearance.setTheme('dark')}>
        choose dark
      </button>
    </>
  )
}

function renderProvider(options: { identity?: string; projectDefault?: AppearanceConfig }) {
  const element = (options: { identity?: string; projectDefault?: AppearanceConfig }) => (
    <AppearanceProvider
      projectIdentity={options.identity}
      projectDefault={options.projectDefault ?? DESK_DEFAULTS.appearance}
    >
      <Probe />
    </AppearanceProvider>
  )
  const view = render(element(options))
  return {
    ...view,
    /** Re-render the same provider with different answers behind it. */
    move: (next: Parameters<typeof element>[0]) => view.rerender(element(next))
  }
}

const effective = () => screen.getByTestId('effective').textContent
const record = (key: string) => window.localStorage.getItem(key)

describe('a choice belongs to the project it was made in', () => {
  it('forgets it when the chassis names a different project, and re-seeds from that one', () => {
    // The review's scenario: one tab, one origin, a chassis that reconnects
    // and reports another root. `chosen` used to be visit-wide, so the
    // re-seed kept A's dark and the write effect put it in B's record — one
    // project's preference in another project's key, permanently.
    window.localStorage.setItem(KEY_B, JSON.stringify({ v: 1, theme: 'light' }))
    const view = renderProvider({ identity: A, projectDefault: LIGHT_COMPACT })
    act(() => screen.getByRole('button', { name: 'choose dark' }).click())
    expect(effective()).toBe('dark/compact')
    expect(JSON.parse(record(KEY_A)!)).toEqual({ v: 1, theme: 'dark' })

    act(() => view.move({ identity: B, projectDefault: LIGHT_COMPACT }))
    // B's own record is what is in force, and B's record is what it was.
    expect(effective()).toBe('light/compact')
    expect(JSON.parse(record(KEY_B)!)).toEqual({ v: 1, theme: 'light' })
    // And A's is left exactly as A left it.
    expect(JSON.parse(record(KEY_A)!)).toEqual({ v: 1, theme: 'dark' })
  })

  it('writes nothing into a project that had no record of its own', () => {
    const view = renderProvider({ identity: A, projectDefault: LIGHT_COMPACT })
    act(() => screen.getByRole('button', { name: 'choose dark' }).click())
    act(() => view.move({ identity: B, projectDefault: LIGHT_COMPACT }))
    expect(record(KEY_B)).toBeNull()
    expect(effective()).toBe('light/compact')
  })

  it('forgets both members, not only the one the re-seed happened to reach', () => {
    // The old code returned early when every member was chosen — before it
    // cleared anything — so the project with the most thoroughly chosen
    // appearance was the one whose choices leaked furthest.
    const view = renderProvider({ identity: A, projectDefault: LIGHT_COMPACT })
    act(() => screen.getByRole('button', { name: 'choose dark' }).click())
    act(() => view.move({ identity: B, projectDefault: { theme: 'system', density: 'comfortable' } }))
    expect(effective()).toBe('system/comfortable')
    expect(record(KEY_B)).toBeNull()
  })

  it('keeps a choice made before the chassis had named any project', () => {
    // The one carry-forward that is legitimate: the provisional key names no
    // project, so a choice made under it was made for whichever project the
    // listing then names — and it is written there rather than dropped.
    const view = renderProvider({ identity: undefined, projectDefault: LIGHT_COMPACT })
    act(() => screen.getByRole('button', { name: 'choose dark' }).click())
    expect(effective()).toBe('dark/compact')
    expect(
      Object.keys(window.localStorage).filter((key) => key.startsWith('jpack-desk:appearance:'))
    ).toEqual([])

    act(() => view.move({ identity: A, projectDefault: LIGHT_COMPACT }))
    expect(effective()).toBe('dark/compact')
    expect(JSON.parse(record(KEY_A)!)).toEqual({ v: 1, theme: 'dark' })
  })
})
