import { msg } from '../i18n'
export const TOOL_LABELS: Record<string, string> = {
  get get_authoring_instructions() { return msg("Read authoring instructions") },
  get get_schema() { return msg("Read pack schema") }, get list_examples() { return msg("Browse examples") }, get get_example() { return msg("Read examples") },
  get validate() { return msg("Validate pack structure") }, get experimental_evaluate() { return msg("Test a decision") },
  get experimental_test_packs() { return msg("Run pack tests") }, get experimental_validate_expectations() { return msg("Validate test expectations") },
  get list_packs() { return msg("List packs") }, get get_pack() { return msg("Read a pack") }, get test_conformance() { return msg("Check conformance") },
  get search_sources() { return msg("Search sources") }, get read_source() { return msg("Read a source") }, get cite_excerpt() { return msg("Record a citation") }
}
