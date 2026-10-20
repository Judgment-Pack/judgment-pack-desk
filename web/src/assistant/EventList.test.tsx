import {cleanup, render, screen, within} from '@testing-library/react'
import {afterEach, describe, expect, it} from 'vitest'
import type {AssistantEvent} from '@judgment-pack/harness/engine'
import {describeEvent, EventList} from './EventList'
import {sourceMessage} from '../i18n/source'

afterEach(cleanup)

// Hand-authored published v0.6 events. Arguments and source records are data,
// and a status line never presents them as instructions or an approval grant.
const events: AssistantEvent[] = [
  {type:'approval_requested', callId:'fixture-call', name:'external_write', tool:'create_issue',
    source:{kind:'custom',id:'fixture',trust:'person'}, args:{secret:'do not render', Fact:false, fact:null}},
  {type:'skill_loaded', skill:{id:'review',name:'Review_policy',source:{kind:'custom',id:'fixture',trust:'person'},digest:'sha256:'+'a'.repeat(64)},bytes:123},
  {type:'skill_unloaded',id:'review'},
  {type:'end'}
]

describe('published approval and skill event presentation', () => {
  it('gives each event an explicit readable status without rendering its payload', () => {
    expect(events.map(event=>describeEvent(event,sourceMessage))).toEqual([
      'Waiting for your approval: create_issue',
      'Loaded skill Review_policy · 123 bytes',
      'Unloaded skill review',
      'the session ended'
    ])
    render(<EventList events={events} />)
    const list=screen.getByRole('list',{name:'What the assistant did'})
    expect(within(list).getAllByRole('listitem').map(line=>line.textContent)).toEqual([
      'Waiting for your approval: create_issue',
      'Loaded skill Review_policy · 123 bytes',
      'Unloaded skill review',
      'the session ended'
    ])
    expect(list.textContent).not.toContain('do not render')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('keeps approval waiting actionable in compact mode and filters observational skill events', () => {
    render(<EventList events={events} compact />)
    expect(screen.getAllByRole('listitem').map(line=>line.textContent)).toEqual([
      'Waiting for your approval: create_issue',
      'the session ended'
    ])
    expect(screen.queryByRole('button')).toBeNull()
  })
})
