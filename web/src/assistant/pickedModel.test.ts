/**
 * The pick a tab remembers, and the three ways it is not honoured.
 *
 * **A preference, and the properties that make it one**: it is per tab, it is
 * keyed on the project the chassis pinned, it never reaches a file, and a value
 * the enabled set no longer holds is treated as absent rather than run.
 *
 * The last of those is the one worth a suite: a set moves — a model is unticked
 * in Admin, an endpoint is replaced — and a run on an id this desk is no longer
 * configured for would be a request nobody enabled, made on the strength of
 * something a browser remembered.
 */
import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  pickedModelKey,
  readPickedModel,
  usePickedModel,
  writePickedModel
} from './pickedModel'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

const ROOT = '/home/someone/work/a-project'
const MODELS = ['a-model', 'a-second-model']

describe('where a pick is kept', () => {
  it('names the project root, so two projects on one origin do not share one', () => {
    expect(pickedModelKey(ROOT)).not.toBe(pickedModelKey('/home/someone/work/another'))
    expect(pickedModelKey(ROOT)).toContain(encodeURIComponent(ROOT))
  })

  it('is sessionStorage and never localStorage, because a pick is per tab', () => {
    writePickedModel(pickedModelKey(ROOT), 'a-model')
    expect(window.sessionStorage.getItem(pickedModelKey(ROOT))).toBe('a-model')
    expect(window.localStorage.getItem(pickedModelKey(ROOT))).toBeNull()
  })

  it('answers nothing where the accessor itself throws', () => {
    // A private window, or a browser set to block site data, throws on the
    // accessor rather than answering null — and a thrown accessor must still
    // leave a working tab.
    vi.stubGlobal('sessionStorage', {
      getItem: () => {
        throw new Error('the browser refused to answer')
      },
      setItem: () => {
        throw new Error('the browser refused to write')
      }
    })
    expect(readPickedModel(pickedModelKey(ROOT), MODELS)).toBeUndefined()
    expect(() => writePickedModel(pickedModelKey(ROOT), 'a-model')).not.toThrow()
  })

  it('treats a value the set no longer holds as absent', () => {
    writePickedModel(pickedModelKey(ROOT), 'a-model-nobody-enables')
    expect(readPickedModel(pickedModelKey(ROOT), MODELS)).toBeUndefined()
  })
})

describe('the pick a run is given', () => {
  const pick = (models: readonly string[], fallback: string | null) =>
    renderHook(() => usePickedModel(models, fallback, ROOT))

  it('opens on the default where nothing has been picked', () => {
    expect(pick(MODELS, 'a-second-model').result.current.model).toBe('a-second-model')
  })

  it('takes a pick, and remembers it for the next mount in this tab', () => {
    const { result } = pick(MODELS, 'a-model')
    act(() => result.current.pick('a-second-model'))
    expect(result.current.model).toBe('a-second-model')
    cleanup()
    expect(pick(MODELS, 'a-model').result.current.model).toBe('a-second-model')
  })

  it('falls back to the default where the set moves under a remembered pick', () => {
    // The endpoint's set is what decides, and it decides at every render: an
    // effect that corrected this afterwards would leave one frame in which the
    // run is about to use a model nothing enables.
    writePickedModel(pickedModelKey(ROOT), 'a-second-model')
    expect(pick(['a-model'], 'a-model').result.current.model).toBe('a-model')
  })

  it('refuses a pick outside the set, which is the same rule the read applies', () => {
    const { result } = pick(MODELS, 'a-model')
    act(() => result.current.pick('a-model-nobody-enables'))
    expect(result.current.model).toBe('a-model')
    expect(window.sessionStorage.getItem(pickedModelKey(ROOT))).toBeNull()
  })

  it('is the empty string where nothing is enabled at all', () => {
    expect(pick([], null).result.current.model).toBe('')
  })
})
