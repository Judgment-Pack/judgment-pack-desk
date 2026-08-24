import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { MatrixProbe } from '../mcp/types'
import { CoverageReport } from './CoverageReport'

afterEach(cleanup)

const coverage: MatrixProbe[] = [
  { probe: 'outcome:proceed', status: 'covered' },
  { probe: 'outcome:decline', status: 'missing', detail: 'no row expects it' },
  { probe: 'boundary:/expense/amountUsd:5000', status: 'missing', detail: 'no row states 5000' },
  { probe: 'no-match', status: 'covered' },
  { probe: 'unknown', status: 'missing', detail: 'no row leaves it unknown' }
]

describe('CoverageReport', () => {
  it('renders every probe family the wire grammar names', () => {
    const { container } = render(<CoverageReport coverage={coverage} />)
    const text = container.textContent ?? ''
    expect(text).toContain('Declared outcomes')
    expect(text).toContain('Boundary probes')
    expect(text).toContain('Resolution reasons')
    expect(text).toContain('/expense/amountUsd')
  })

  it('leads with what is missing', () => {
    const { container } = render(<CoverageReport coverage={coverage} />)
    expect(container.querySelector('.note-warn')).not.toBeNull()
  })

  it('invents no cause for an absent report', () => {
    const { container } = render(<CoverageReport coverage={undefined} />)
    const text = container.textContent ?? ''
    expect(text).toContain('No coverage was reported')
    expect(text).not.toContain('did not load')
  })
})
