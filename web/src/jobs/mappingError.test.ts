import { afterEach, expect, it } from 'vitest'
import { CanonError, type CanonErrorCode } from '../dialect/canon'
import { languageReady, setLanguage } from '../i18n'
import { LANGUAGES } from '../i18n/locales'
import { mappingErrorMessage } from './mappingError'
const codes:CanonErrorCode[]=['invalid_unicode','integer_literal','integer_range','nesting_limit','invalid_json','duplicate_member','non_json_object','non_string_member','accessor_member','non_json_value']
afterEach(async()=>{setLanguage('en');await languageReady()})
it.each(LANGUAGES)('$id: translates every canonical rule without leaking a code or template',async({id})=>{
 setLanguage('en');await languageReady()
 const errors=codes.map(code=>new CanonError(code,{offset:3,member:'sample'}))
 const english=errors.map(mappingErrorMessage)
 setLanguage(id);await languageReady()
 for(const [index,error] of errors.entries()) {
  const text=mappingErrorMessage(error)
  expect(text).toContain(error.section)
  expect(text).not.toContain(error.code)
  expect(text).not.toContain('{{')
  if(id!=='en')expect(text).not.toBe(english[index])
 }
})
it('leaves other errors in their existing words',()=>{
 expect(mappingErrorMessage(Error('Runner refusal'))).toBe('Runner refusal')
})
