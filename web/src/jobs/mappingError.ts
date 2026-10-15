import { CanonError } from '../dialect/canon'
import { msg } from '../i18n'

/** Translate canonical rule codes only where Jobs presents an error. */
export function mappingErrorMessage(error: unknown): string {
  if (!(error instanceof CanonError)) return error instanceof Error ? error.message : msg('The inputs could not be mapped.')
  const values = { section: error.section, ...error.details }
  switch (error.code) {
    case 'invalid_unicode': return msg('Use valid Unicode characters without lone surrogates ({{section}}).', values)
    case 'integer_literal': return msg('Write integers without fractions or exponents ({{section}}).', values)
    case 'integer_range': return msg('Use whole numbers between −9007199254740991 and 9007199254740991 ({{section}}).', values)
    case 'nesting_limit': return msg('JSON may contain at most 64 nested containers ({{section}}).', values)
    case 'invalid_json': return msg('Enter one valid JSON value. Check position {{offset}} ({{section}}).', values)
    case 'duplicate_member': return msg('JSON member “{{member}}” appears more than once ({{section}}).', values)
    case 'non_json_object':
    case 'non_string_member':
    case 'accessor_member':
    case 'non_json_value': return msg('Use only JSON values with string member names and plain values ({{section}}).', values)
  }
}
