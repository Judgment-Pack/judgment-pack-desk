// Run from web with: node --import tsx scripts/dialect-roundtrip.mjs
import { cp, mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { canonicalize, canonicalPreparation, dialectDigest, parseJSON, preparationDigest } from '../src/dialect/canon.ts'

const source = new URL('../../.spec/conformance/dialect/vectors/valid/', import.meta.url)
const directory = await mkdtemp(join(tmpdir(), 'desk-dialect-'))
try {
  await cp(source, directory, {recursive:true})
  const mapping = parseJSON(await readFile(join(directory, 'mapping.json'), 'utf8'))
  const preparation = parseJSON(await readFile(join(directory, 'preparation.json'), 'utf8'))
  if (await dialectDigest(mapping) !== preparation.mappingDigest) throw Error('Dialect digest differs from the pinned preparation')
  if (await preparationDigest(preparation) !== (await readFile(join(directory, 'preparation.sha256'), 'utf8')).trim()) throw Error('Preparation digest differs from the pinned vector')
  await writeFile(join(directory, 'mapping.json'), canonicalize(mapping))
  await writeFile(join(directory, 'preparation.json'), canonicalPreparation(preparation))
  // This pinned vector is export v5. Read the record's retained bytes; never
  // re-encode run.audit, whose binding order is itself part of the contract.
  const exported = JSON.parse(await readFile(join(directory, 'run.json'), 'utf8'))
  if (exported.version !== 5 || typeof exported.run.auditBytes !== 'string') throw Error('Expected the pinned v5 record bytes')
  await writeFile(join(directory, 'record.json'), Buffer.from(exported.run.auditBytes, 'base64'))
  process.stdout.write(directory + '\n')
} catch (error) {
  await rm(directory, {recursive:true, force:true})
  throw error
}
