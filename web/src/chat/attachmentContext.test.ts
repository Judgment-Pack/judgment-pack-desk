import { describe, expect, it } from 'vitest'
import { ContextLedger } from '@judgment-pack/harness/context/ledger'
import { CONTEXT_DEFAULTS } from '@judgment-pack/harness/context/policy'
import { signedDocument } from '../documents/__fixtures__/signedDocument'
import { verifyDocument } from '../documents/client'
import { ATTACHMENT_LEDGER_BYTES, attachmentDocument, attachmentForReview, attachmentNote, documentPages, ledgerBytes, textPages } from './attachmentContext'

const TEXT = 'Line one of the policy.\nApplicants with 1,560 hours qualify.\nLine three.'
const textFile = { id: 'f1e2d3c4-0000-4000-8000-000000000001', name: 'policy.txt', text: TEXT }

describe('a chat attachment in the run\'s ledger', () => {
  it('names a text file in the message and puts its text in the ledger, where read_window reads it by line', async () => {
    const attached = textPages(textFile)
    const note = attachmentNote(attached)
    expect(note).toContain('Attached file: policy.txt')
    expect(note).toContain('read it with read_window')
    expect(note).not.toContain('1,560')
    const document = attachmentDocument(attached)
    expect(document).toMatchObject({ id: `attachment-${textFile.id}`, kind: 'artifact', label: 'Attached file: policy.txt', value: { pages: [TEXT] } })
    const ledger = new ContextLedger(CONTEXT_DEFAULTS)
    const entry = await ledger.retain(document, true)
    const read = ledger.readUnits({ id: entry.id, unit: 'line', offset: 1, limit: 1 })
    expect(read.text).toBe('2\tApplicants with 1,560 hours qualify.')
    expect(ledger.search({ pattern: '1,560', scope: 'attachments' }).matches[0]).toMatchObject({ id: entry.id, line: 2 })
  })

  it('puts a document\'s selected pages in the ledger, each opening with its citation, and names it with its pages', async () => {
    const { object, pin, reference } = await signedDocument()
    const verified = await verifyDocument(object, pin)
    const file = { id: reference.id, name: 'notes.txt', text: '', document: reference }
    const attached = documentPages(file, verified)
    const note = attachmentNote(attached)
    expect(note).toContain(`Attached document: notes.txt, ${reference.pages.length} selected page`)
    expect(note).toContain('each page opens with the citation that names it')
    const pages = (attachmentDocument(attached).value as { pages: string[] }).pages
    expect(pages).toHaveLength(reference.pages.length)
    pages.forEach((page, index) => {
      const citation = `attachment:${reference.id}/${reference.digest}/page/${reference.pages[index]}`
      expect(page.startsWith(`[page ${reference.pages[index]}; cite it as ${citation}]\n`)).toBe(true)
      expect(note).not.toContain(page.slice(page.indexOf('\n') + 1))
    })
    expect(attachmentForReview(attached)).toMatchObject({ name: 'notes.txt', pages: [{ page: reference.pages[0], citation: expect.stringContaining('attachment:') }] })
  })

  it('retains why a document could not be verified, never its text', () => {
    const file = { id: textFile.id, name: 'gone.pdf', text: '', document: { id: textFile.id, digest: 'sha256:' + 'a'.repeat(64), pages: [1], allowPartial: false } }
    const attached = { file, pages: [], unavailable: 'this document could not be verified now' }
    expect(attachmentDocument(attached).value).toEqual({ unavailable: 'this document could not be verified now' })
    expect(attachmentForReview(attached)).toEqual({ name: 'gone.pdf', unavailable: 'this document could not be verified now' })
  })

  it('counts a message\'s attachments as the ledger counts them', () => {
    const big = { ...textFile, text: 'x'.repeat(ATTACHMENT_LEDGER_BYTES / 4) }
    const four = [1, 2, 3, 4].map(n => attachmentDocument(textPages({ ...big, id: `${n}` })))
    expect(ledgerBytes(four)).toBe(4 * JSON.stringify({ pages: [big.text] }).length)
    expect(ledgerBytes(four)).toBeGreaterThan(ATTACHMENT_LEDGER_BYTES)
    expect(ledgerBytes(four.slice(0, 3))).toBeLessThan(ATTACHMENT_LEDGER_BYTES)
  })
})
