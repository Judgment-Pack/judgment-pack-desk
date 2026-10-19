import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { CriticDetails } from './CriticDetails'
import { RefutationReport } from './ProposalReport'
afterEach(cleanup)

const figures = { model: 'cheap-critic', unit: 'model-requests' as const, requests: 4, inputTokens: 1200, outputTokens: 90 }

it('says what was compared, what was not, what the review cost and which model ran it', () => {
  render(<CriticDetails record={{ compared: 5, disagreed: 2, incomplete: ['c8', 'c9'], figures, runModel: 'main-model' }} />)
  expect(screen.getByText('Declared cases: 5 compared, 2 differ from the expected outcome.')).toBeTruthy()
  expect(screen.getByRole('status').textContent).toContain('this review cannot say the proposal was not refuted. Not compared: c8, c9')
  expect(screen.getByText(/Review cost: 4 model requests on cheap-critic · tokens in 1200, out 90/)).toBeTruthy()
  expect(screen.getByText(/The review ran on cheap-critic, the model this connection names for reviews, and the answer on main-model\./)).toBeTruthy()
})

it('names no other model where the review ran on the run\'s own', () => {
  render(<CriticDetails record={{ compared: 1, disagreed: 0, incomplete: [], figures: { ...figures, model: 'main-model' }, runModel: 'main-model' }} />)
  expect(screen.queryByText(/names for reviews/)).toBeNull()
  expect(screen.queryByRole('status')).toBeNull()
})

it('reports a critique with an uncompared case as incomplete, never as not refuted', () => {
  const events: AssistantEvent[] = [{ type: 'critique', refuted: true, checks: [{ tool: 'validate', status: 'valid' }], text: 'validate valid; incomplete, 1 declared case(s) not compared', incomplete: ['c2'], cases: [], figures }]
  render(<RefutationReport events={events} runModel="main-model" />)
  expect(screen.getByText(/The review is incomplete\. Nothing here says this proposal holds\./)).toBeTruthy()
  expect(screen.queryByText(/did not refute this proposal/)).toBeNull()
  expect(screen.queryByText(/The runtime refuted this proposal/)).toBeNull()
  expect(screen.getByText(/Not compared: c2/)).toBeTruthy()
})

it('still reports a complete critique as the runtime\'s verdict', () => {
  const events: AssistantEvent[] = [{ type: 'critique', refuted: false, checks: [{ tool: 'validate', status: 'valid' }], text: 'validate valid', incomplete: [], cases: [{ case: 'c1', source: 'test', tool: 'experimental_test_cases', agrees: true, expected: { disposition: { kind: 'approve' } }, actual: { disposition: { kind: 'approve' } } }], figures }]
  render(<RefutationReport events={events} />)
  expect(screen.getByText(/The runtime did not refute this proposal\./)).toBeTruthy()
  expect(screen.getByText('Declared cases: 1 compared, 0 differ from the expected outcome.')).toBeTruthy()
})
