import { msg } from '../i18n'
export const TOOL_LABELS: Record<string, string> = {
 get get_dialect_contract(){return msg("Read dialect contract")},
 get get_dialect_schema(){return msg("Read dialect schema")},
 get get_dialect_reference(){return msg("Read dialect reference")},
 get dialect_validate(){return msg("Validate dialect")},
 get dialect_check(){return msg("Check dialect coverage")},
 get dialect_canonicalize(){return msg("Canonicalize dialect")},
 get dialect_digest(){return msg("Calculate dialect digest")},
 get dialect_init(){return msg("Create dialect starter")},

  get request_budget_increase(){return msg('Request more time')},
  get update_plan(){return msg('Update task progress')},
 get propose_plan(){return msg('Propose plan')},
 get recall_memory(){return msg('Recall project memory')},
 get remember_memory(){return msg('Remember a preference')},
 get list_context(){return msg('Browse retained context')},
 get read_context(){return msg('Read retained context')},
 get read_pack_tests(){return msg('Read pack tests')},
 get propose_test_cases(){return msg('Propose test cases')},
 get ask_user(){return msg('Ask a question')},
 get delegate_task(){return msg('Delegated task')},
 get delegate_tasks(){return msg('Delegate tasks')},
 get request_dialect_research(){return msg('Request dialect research')},
 get get_dialect_recipe_instructions(){return msg('Read dialect recipe instructions')},
 get list_dialects(){return msg('List dialects')},
 get read_dialect_recipe(){return msg('Read dialect recipe')},
 get read_dialect_records(){return msg('Read records')},
  get read_dialect_activity(){return msg('Read dialect activity')},
  get read_dialect(){return msg('Read dialect')},
  get propose_dialect(){return msg('Propose dialect')},
  get propose_dialect_action(){return msg('Propose dialect action')},
  get propose_dialect_recipe(){return msg('Propose dialect recipe')},
  get list_decisions() { return msg('List decisions') },
  get read_decision() { return msg('Read decision') },
  get get_graph_authoring_instructions() { return msg('Read graph instructions') },
  get graph_validate() { return msg('Validate graph') },
  get graph_explain() { return msg('Explain graph plan') },
  get propose_graph() { return msg('Propose graph') },
  get graph_rehearse() { return msg('Rehearse graph') },
  get get_authoring_instructions() { return msg("Read authoring instructions") },
  get get_artifact_version_options() { return msg("Calculate version options") }, get get_schema() { return msg("Read pack schema") }, get list_examples() { return msg("Browse examples") }, get get_example() { return msg("Read examples") },
  get validate() { return msg("Validate pack structure") }, get experimental_evaluate() { return msg("Test a decision") },
  get experimental_test_packs() { return msg("Run pack tests") }, get experimental_validate_expectations() { return msg("Validate test expectations") },
  get list_packs() { return msg("List packs") }, get get_pack() { return msg("Read a pack") }, get test_conformance() { return msg("Check conformance") },
  get search_sources() { return msg("Search sources") }, get read_source() { return msg("Read a source") }, get cite_excerpt() { return msg("Record a citation") },
  get explore_website(){return msg("Explore website")},
  get read_link() { return msg("Read a link") }
}

// Saved step names predate the Dialects workspace; keep their labels readable.
Object.defineProperty(TOOL_LABELS, 'get_input_recipe_instructions', {get:()=>TOOL_LABELS.get_dialect_recipe_instructions})
Object.defineProperty(TOOL_LABELS, 'list_inputs', {get:()=>TOOL_LABELS.list_dialects})
Object.defineProperty(TOOL_LABELS, 'read_input_recipe', {get:()=>TOOL_LABELS.read_dialect_recipe})
Object.defineProperty(TOOL_LABELS, 'propose_input_recipe', {get:()=>TOOL_LABELS.propose_dialect_recipe})
Object.defineProperty(TOOL_LABELS, 'request_input_research', {get:()=>TOOL_LABELS.request_dialect_research})
Object.defineProperty(TOOL_LABELS, 'read_input_records', {get:()=>TOOL_LABELS.read_dialect_records})
Object.defineProperty(TOOL_LABELS, 'read_input_activity', {get:()=>TOOL_LABELS.read_dialect_activity})
Object.defineProperty(TOOL_LABELS, 'propose_input_action', {get:()=>TOOL_LABELS.propose_dialect_action})
