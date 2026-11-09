import {expect, it} from 'vitest'
import {proposePackTool, withProposePack} from './proposePack'

it('declares the propose_pack owner taxonomy the server-side authoring tool declares', async () => {
  const tool = proposePackTool()
  expect(tool.taxonomy).toEqual({operation: 'propose', effect: 'propose'})
  expect(tool.effect).toBe('proposal')
  expect(tool.delegation).toBeUndefined()
  await expect(tool.execute({documentJson: '{}'}, new AbortController().signal)).rejects.toThrow('read by the engine')
  expect(withProposePack([tool]).length).toBe(1)
})
