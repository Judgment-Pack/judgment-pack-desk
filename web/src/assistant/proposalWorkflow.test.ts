import { expect, it, vi } from 'vitest'
import { runProposalWorkflow, type ProposalProgress, type ProposalReply } from './proposalWorkflow'
const bad = { cases: [{ id: 'x', name: 'Unsupported' }] }
const good = { cases: [{ id: 'x', focus: 'Supported' }] }
const finding = {
  code: 'MATRIX-MEMBER',
  path: '/cases/0/name',
  message: 'Unsupported member name; use focus.',
}
const reply = (document: unknown): ProposalReply => ({ document, unknowns: [], message: 'Proposed.' })
it('persists invalid input and exact findings before requesting a correction', async () => {
  const snapshots: ProposalProgress[] = []
  const generate = vi
    .fn()
    .mockResolvedValueOnce(reply(bad))
    .mockImplementationOnce(async (prompt: string) => {
      expect(snapshots.at(-1)?.attempts[0]?.findings).toEqual([finding])
      expect(prompt).toContain('/cases/0/name')
      expect(prompt).toContain(JSON.stringify(bad))
      return reply(good)
    })
  const result = await runProposalWorkflow(
    'design',
    {
      generate,
      validate: async (d) => (d === bad ? [finding] : []),
      checkpoint: async (p) => {
        snapshots.push(p)
      },
    },
    new AbortController().signal,
  )
  expect(result.state).toBe('ready')
  expect(result.attempts).toHaveLength(2)
  expect(result.message).toContain('No changes have been saved')
  expect(snapshots.find((s) => s.state === 'checking')?.attempts[0]?.document).toEqual(bad)
})
it('bounds correction requests and retains all rejected proposals', async () => {
  const generate = vi.fn().mockResolvedValue(reply(bad))
  const result = await runProposalWorkflow(
    'design',
    { generate, validate: async () => [finding], checkpoint: async () => {} },
    new AbortController().signal,
  )
  expect(generate).toHaveBeenCalledTimes(3)
  expect(result.state).toBe('blocked')
  expect(result.attempts).toHaveLength(3)
})
it('stops before another model request when canceled during validation', async () => {
  const controller = new AbortController()
  const generate = vi.fn().mockResolvedValue(reply(bad))
  const result = await runProposalWorkflow(
    'design',
    {
      generate,
      validate: async () => {
        controller.abort()
        return [finding]
      },
      checkpoint: async () => {},
    },
    controller.signal,
  )
  expect(result.state).toBe('stopped')
  expect(generate).toHaveBeenCalledTimes(1)
  expect(result.attempts[0]?.document).toEqual(bad)
})
it('preserves a proposal when validation transport fails and does not ask the model to fix connectivity', async () => {
  const generate = vi.fn().mockResolvedValue(reply(good))
  const result = await runProposalWorkflow(
    'design',
    {
      generate,
      validate: async () => {
        throw Error('Runtime disconnected')
      },
      checkpoint: async () => {},
    },
    new AbortController().signal,
  )
  expect(result.state).toBe('blocked')
  expect(result.message).toBe('Runtime disconnected')
  expect(result.attempts[0]?.document).toEqual(good)
  expect(generate).toHaveBeenCalledTimes(1)
})
it('never exposes ready when preservation finds changed meaning', async () => {
  const generate = vi.fn().mockResolvedValueOnce(reply(bad)).mockResolvedValue(reply(good))
  const result = await runProposalWorkflow(
    'design',
    {
      generate,
      validate: async (d) => (d === bad ? [finding] : []),
      preserve: () => [{ code: 'MEANING', path: '/cases/0', message: 'Expected result changed' }],
      checkpoint: async () => {},
    },
    new AbortController().signal,
  )
  expect(result.state).toBe('blocked')
})
it('does not report success when checkpoint storage fails', async () => {
  const generate = vi.fn().mockResolvedValue(reply(good))
  await expect(
    runProposalWorkflow(
      'design',
      {
        generate,
        validate: async () => [],
        checkpoint: async () => {
          throw Error('Storage conflict')
        },
      },
      new AbortController().signal,
    ),
  ).rejects.toThrow('Storage conflict')
  expect(generate).not.toHaveBeenCalled()
})
it('preserves an ordinary explanatory answer without inventing a proposal or retrying', async () => {
  const generate = vi.fn().mockResolvedValue({ message: 'Please clarify the policy.', unknowns: [] })
  const result = await runProposalWorkflow(
    'explain',
    { generate, validate: vi.fn(), checkpoint: async () => {} },
    new AbortController().signal,
  )
  expect(result.state).toBe('answered')
  expect(result.attempts).toEqual([])
  expect(generate).toHaveBeenCalledTimes(1)
})
