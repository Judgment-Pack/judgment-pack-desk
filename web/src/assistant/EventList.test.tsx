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

// Hand-authored v0.9.0 observations. Payloads remain in the canonical run record;
// rendering them does not accept memory, apply an edit, or authorize a new call.
const releaseEvents: AssistantEvent[] = [
  {type:'steered',bytes:12,atRequest:2},
  {type:'compacted',turns:2,bytes:45,digest:'sha256:'+'a'.repeat(64),mode:'index'},
  {type:'policy_resolved',members:{}},
  {type:'critic_decided',mode:'off',ran:false,reason:'off'},
  {type:'critic_decided',mode:'on',ran:true,reason:'person'},
  {type:'memory_proposed',memory:{id:'memory-one',kind:'fact',text:'private proposed text',source:{run:'turn-one',proposedBy:'model'},scope:'project',digest:'sha256:'+'b'.repeat(64),createdAt:'2026-10-22T00:00:00Z',state:'proposed'}},
  {type:'memory_recalled',ids:['memory-one'],digests:['sha256:'+'b'.repeat(64)],bytes:42},
  {type:'edit_applied',document:'draft-one',line:1,bytes:4,valid:true},
  {type:'edit_refused',reason:'private refusal details'},
  {type:'output_guarded',where:'message',pattern:'private-pattern',action:'redact'},
  {type:'output_guarded',where:'tool argument',pattern:'private-pattern',action:'refuse'},
  {type:'export_written',records:1,bytes:45,digest:'sha256:'+'c'.repeat(64)},
  {type:'connection_switched',role:'author',from:'private-first',to:'private-second',reason:'unavailable'},
  {type:'usage',usage:{rows:[]}},
  {type:'recorded',id:'private-recording',bytes:45}
]

describe('published v0.9 event observations', () => {
  it('renders each released kind without exposing private payloads or granting an action', () => {
    const expected = [
      'New guidance delivered', 'Earlier context compacted', 'Run settings resolved',
      'Review skipped · off · Review is off', 'Review ran · on · Chat setting', 'Memory proposed for review',
      'Project memory recalled', 'Draft edit validated; nothing was saved',
      'Draft edit refused', 'Sensitive output redacted', 'Sensitive output refused',
      'Run records exported', 'Model connection changed', 'Model usage recorded',
      'Run recording saved'
    ]
    expect(releaseEvents.map(event=>describeEvent(event,sourceMessage))).toEqual(expected)
    render(<EventList events={releaseEvents} />)
    const list=screen.getByRole('list',{name:'What the assistant did'})
    expect(within(list).getAllByRole('listitem').map(line=>line.textContent)).toEqual(expected)
    expect(list.textContent).not.toContain('private')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('keeps observations out of the compact action list', () => {
    render(<EventList events={releaseEvents} compact />)
    expect(screen.queryByRole('list')).toBeNull()
  })
})
