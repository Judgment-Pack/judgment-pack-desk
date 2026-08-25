import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { EvaluationView } from './EvaluationView'
import { MatrixRowList } from './MatrixRowList'
import { BARE_EVALUATION, FULL_EVALUATION, MATRIX_ROWS } from './extractionFixtures'
import { BARE_GOLDEN, FULL_GOLDEN, ROWS_GOLDEN } from './extractionGoldens'

afterEach(cleanup)

/**
 * The extraction, pinned permanently.
 *
 * `TracePanel` and `TargetSide` moved out of these two views so that the graph
 * matrix could render a trace and a target pair through the same code rather
 * than through a second copy. A move like that is supposed to change nothing
 * about what these two views emit — and "supposed to" is not evidence.
 *
 * So these compare the rendered DOM against the DOM the *pre-extraction* tree
 * produced for the same fixtures, captured at c09676d. Anything the move
 * dropped, reordered, or quietly reworded fails here, including in the optional
 * branches nobody thinks to look at: the rehearsal banner, the draft-RFC panel,
 * the handoff-target aside, an unnamed trace entry, a suppressed one, a
 * repeated stage, a refusal comparison, and all three target-report states.
 *
 * A deliberate change to either view will fail these too. That is intended: the
 * golden diff is where such a change gets looked at.
 */
describe('the extracted renderers emit what they emitted before the extraction', () => {
  it('renders an evaluation with every optional member exactly as it did', () => {
    const { container } = render(<EvaluationView payload={FULL_EVALUATION} />)
    expect(container.innerHTML).toBe(FULL_GOLDEN)
  })

  it('renders an evaluation with none of them exactly as it did', () => {
    const { container } = render(<EvaluationView payload={BARE_EVALUATION} />)
    expect(container.innerHTML).toBe(BARE_GOLDEN)
  })

  it('renders every matrix row shape exactly as it did', () => {
    const { container } = render(<MatrixRowList rows={MATRIX_ROWS} />)
    expect(container.innerHTML).toBe(ROWS_GOLDEN)
  })
})
