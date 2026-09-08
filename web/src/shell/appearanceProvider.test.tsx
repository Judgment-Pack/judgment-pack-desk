/**
 * The provider's lifetime: which project a choice belongs to, and when this
 * desk knows enough to paint one.
 *
 * The store's own rules are proved without React in `appearanceState.test.ts`.
 * What needs a render is everything that happens *between* mount and the two
 * answers this desk waits for — the chassis' project root and the project's own
 * configuration file — because both arrive after the first paint and each of
 * them has been a defect.
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
      <p data-testid="effective">{`${appearance.theme ?? '—'}/${appearance.density ?? '—'}`}</p>
      <p data-testid="default">
        {appearance.projectDefault === undefined
          ? 'not known yet'
          : `${appearance.projectDefault.theme}/${appearance.projectDefault.density}`}
      </p>
      <button type="button" onClick={() => appearance.setTheme('dark')}>
        choose dark
      </button>
    </>
  )
}

interface Answers {
  identity?: string
  projectDefault?: AppearanceConfig
  projectDefaultKnown?: boolean
}

function renderProvider(options: Answers) {
  const element = (options: Answers) => (
    <AppearanceProvider
      projectIdentity={options.identity}
      projectDefault={options.projectDefault ?? DESK_DEFAULTS.appearance}
      projectDefaultKnown={options.projectDefaultKnown ?? true}
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
const shownDefault = () => screen.getByTestId('default').textContent
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
    // Their own answer applies at once — nothing below it in the ladder can
    // change it — while the density, which they have not chosen and which
    // cannot be read yet, is still not known.
    expect(effective()).toBe('dark/—')
    expect(
      Object.keys(window.localStorage).filter((key) => key.startsWith('jpack-desk:appearance:'))
    ).toEqual([])

    act(() => view.move({ identity: A, projectDefault: LIGHT_COMPACT }))
    expect(effective()).toBe('dark/compact')
    expect(JSON.parse(record(KEY_A)!)).toEqual({ v: 1, theme: 'dark' })
  })
})

/** Every value this desk wrote onto the root element, in order. */
function recordApplications(): string[] {
  const applied: string[] = []
  const root = document.documentElement
  const set = root.setAttribute.bind(root)
  const remove = root.removeAttribute.bind(root)
  root.setAttribute = (name: string, value: string) => {
    if (name === 'data-theme') applied.push(value)
    set(name, value)
  }
  root.removeAttribute = (name: string) => {
    if (name === 'data-theme') applied.push('(removed)')
    remove(name)
  }
  return applied
}

describe('what this desk paints before it knows', () => {
  it('applies the viewer’s own answer once, and no default it has not read', () => {
    // The review's proof. The record cannot be read until the listing supplies
    // the root, and the project's default is the schema's until the file has
    // been read — so a provider that painted while it waited applied `system`,
    // then the file's `light`, then the stored `dark`: three applications for
    // one load and two of them values nobody chose.
    window.localStorage.setItem(KEY_A, JSON.stringify({ v: 1, theme: 'dark' }))
    const applied = recordApplications()

    const view = renderProvider({ identity: undefined, projectDefaultKnown: false })
    expect(applied).toEqual([])
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
    expect(effective()).toBe('—/—')
    expect(shownDefault()).toBe('not known yet')

    // The listing lands. This browser's own answer is readable now, and it is
    // the top of the ladder — no default can change it — so it applies. The
    // density it does not carry is still nobody's, and is not painted.
    act(() => view.move({ identity: A, projectDefaultKnown: false }))
    expect(applied).toEqual(['dark'])
    expect(effective()).toBe('dark/—')
    expect(shownDefault()).toBe('not known yet')

    // And then the file, which changes the density and not the theme.
    act(() => view.move({ identity: A, projectDefault: LIGHT_COMPACT, projectDefaultKnown: true }))
    expect(applied).toEqual(['dark'])
    expect(effective()).toBe('dark/compact')
    expect(shownDefault()).toBe('light/compact')
    // Never the schema's, and never the file's over a viewer who had answered.
    expect(applied).not.toContain('system')
    expect(applied).not.toContain('light')
  })

  it('waits for the root even when the file answered first', () => {
    // The other order, and the one that ends in the wrong place on its own:
    // with the file in and the root still unknown, the project's default is
    // the only readable thing — and applying it would paint `light` over a
    // viewer whose stored answer is `dark`.
    window.localStorage.setItem(KEY_A, JSON.stringify({ v: 1, theme: 'dark' }))
    const applied = recordApplications()
    const view = renderProvider({
      identity: undefined,
      projectDefault: LIGHT_COMPACT,
      projectDefaultKnown: true
    })
    expect(applied).toEqual([])
    expect(effective()).toBe('—/—')
    // The default *is* known here, and is named: it is what is in force that
    // is not, because this browser's own answer has not been read.
    expect(shownDefault()).toBe('light/compact')

    act(() => view.move({ identity: A, projectDefault: LIGHT_COMPACT, projectDefaultKnown: true }))
    expect(applied).toEqual(['dark'])
  })

  it('applies the file’s value once, for a browser that has answered nothing', () => {
    const applied = recordApplications()
    const view = renderProvider({ identity: undefined, projectDefaultKnown: false })
    act(() => view.move({ identity: A, projectDefaultKnown: false }))
    // The root is in and this browser holds no preference — but what it would
    // fall back to has not been read, so there is still nothing to say.
    expect(applied).toEqual([])
    expect(effective()).toBe('—/—')

    act(() => view.move({ identity: A, projectDefault: LIGHT_COMPACT, projectDefaultKnown: true }))
    expect(applied).toEqual(['light'])
    expect(effective()).toBe('light/compact')
  })

  it('leaves an attribute it did not write exactly where it found it', () => {
    // Not knowing is not a reason to clear: an attribute this desk has not yet
    // decided about is not this desk's to remove, and a page that stripped it
    // on mount would be making a decision under cover of making none.
    document.documentElement.setAttribute('data-theme', 'dark')
    renderProvider({ identity: undefined, projectDefaultKnown: false })
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  })
})
