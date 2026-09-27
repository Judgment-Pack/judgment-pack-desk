import {expect,it} from 'vitest'
import {scheduleInstant,scheduleWallTime} from './scheduleTime'
it('uses the selected zone and does not invent an instant in a daylight-saving gap',()=>{
 expect(scheduleInstant('2026-03-08T02:30','America/Toronto')).toBeUndefined()
 expect(scheduleInstant('2026-11-01T01:30','America/Toronto')).toBe('2026-11-01T05:30:00.000Z')
 expect(scheduleWallTime('2026-11-01T05:30:00Z','America/Toronto')).toBe('2026-11-01T01:30')
 expect(scheduleInstant('2026-09-26T09:00','Asia/Kolkata')).toBe('2026-09-26T03:30:00.000Z')
 expect(scheduleInstant('2026-02-30T09:00','UTC')).toBeUndefined()
 expect(scheduleInstant('2026-09-26T09:00','invalid')).toBeUndefined()
})
