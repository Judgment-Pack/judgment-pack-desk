/**
 * The console buffer, and the one line in `McpProvider` that feeds it.
 *
 * The provider's `desk/fileChanged` handler is installed on a live SDK Client
 * over a real socket, which a component test has no business standing up — so
 * the feed is asserted **by reading the source**, and that is a weak guard,
 * labelled here as one. It catches the call being deleted or defanged, which is
 * the regression that would leave a channel called Files saying nothing; it
 * cannot catch a handler that stops being reached for some other reason.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import {
  consoleSnapshot,
  forgetConsole,
  recordConnection,
  recordFileChange
} from './consoleLog'

afterEach(forgetConsole)

describe('the console buffer', () => {
  it('records one entry per file change, carrying the path and nothing else', () => {
    recordFileChange('packs/intake-triage.json')
    expect(consoleSnapshot()).toHaveLength(1)
    expect(consoleSnapshot()[0]).toMatchObject({
      channel: 'files',
      text: 'packs/intake-triage.json'
    })
  })

  it('says so where the notification carried no path, rather than logging an empty line', () => {
    recordFileChange('')
    expect(consoleSnapshot()[0]!.text).toContain('no path')
  })

  it('drops an identical consecutive connection line', () => {
    // StrictMode runs the watching effect mount → cleanup → mount, so one
    // transition is offered twice.
    recordConnection('ready · connection 1')
    recordConnection('ready · connection 1')
    recordConnection('reconnecting · connection 1 (attempt 1)')
    expect(consoleSnapshot().map((entry) => entry.text)).toEqual([
      'ready · connection 1',
      'reconnecting · connection 1 (attempt 1)'
    ])
  })

  it('is bounded, and keeps the newest entries', () => {
    for (let index = 0; index < 520; index += 1) recordFileChange(`file-${index}`)
    const entries = consoleSnapshot()
    expect(entries).toHaveLength(500)
    expect(entries[entries.length - 1]!.text).toBe('file-519')
    expect(entries[0]!.text).toBe('file-20')
  })
})

describe('(weak, source-read) the provider’s one line into the Files channel', () => {
  it('calls recordFileChange with the notification’s path, inside that branch', () => {
    const source = readFileSync(join(import.meta.dirname, '../mcp/McpProvider.tsx'), 'utf8')
    const branch = source.slice(
      source.indexOf("if (notification.method !== 'desk/fileChanged') return"),
      source.indexOf('await queryClient.cancelQueries()')
    )
    expect(branch).toMatch(/recordFileChange\(\s*String\(/)
    expect(branch).toContain('path')
  })
})
