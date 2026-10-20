#!/usr/bin/env bash
# scripts/dev/jobs-companion.sh STEP
# The steps of dev.json that run the Desk's tests against the attested official companions the
# component lock names (the Runtime, the Runner, the Gateway), and against the dialect Gateway
# bundle. Each STEP installs what it needs once into a scratch directory (RUNNER_TEMP under CI),
# so any step can be run alone with `scripts/dev/check.sh --only NAME`. Needs network access,
# gh, and git; the downloads read GH_TOKEN.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
. "$root/scripts/dev/gateway-fixture.sh"
cd "$root"

official="$tmp/jobs-official"
dialect="$tmp/dialect-gateway"
old_desk_bin="$tmp/jpack-policy-old-desk"

# The attested official companion archives, installed once.
install_official() {
  [ -f "$official/component-artifacts.json" ] && return 0
  rm -rf "$official" "$tmp/jobs-downloads"
  python3 "$root/scripts/dev/official-companions.py" "$official" "$tmp/jobs-downloads"
}

# The Gateway bundle the dialect journeys use, built from the pinned source.
build_dialect_gateway() {
  prepare_gateway_fixture
}

# The historical Desk's command line, for the rollback refusal test. Only that command line is
# exercised: the embedded page is an inert stand-in, not a reconstructed application.
build_old_desk() {
  [ -x "$old_desk_bin" ] && return 0
  checkout_at "$tmp/old-desk" judgment-pack-desk ef1110a8ad618506012cca3b367f2b49a9ae3766
  mkdir -p "$tmp/old-desk/web/dist"
  printf '<!doctype html><title>CLI fixture only</title>\n' > "$tmp/old-desk/web/dist/index.html"
  go -C "$tmp/old-desk" build -trimpath -o "$old_desk_bin" .
}

# The Exchange release the signed-release tests read, with its release trust (this applies only
# to that checkout, never to application imports, installed profiles or global Git configuration).
exchange_release() {
  local dir=$1
  checkout_at "$dir" judgment-pack-exchange 565ddf477c7ed986906409f19bcf8fccfaafa8a5 full
  git -C "$dir" config gpg.ssh.allowedSignersFile "$root/.github/testdata/exchange-release-allowed-signers"
}

# The Gateway acquisition fixture: the official Gateway archive with the fixture's own program
# added, and true provenance for both.
build_acquisition_fixture() {
ACQ="$tmp" python3 - <<'PYTHON'
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
spec = importlib.util.spec_from_file_location('components', 'scripts/component-releases.py')
components = importlib.util.module_from_spec(spec)
spec.loader.exec_module(components)
gateway = components.read_plan()['components']['gateway']
temporary = Path(os.environ['ACQ'])
downloads = temporary / 'acquisition-downloads' / 'gateway'
archive = 'judgment-pack-gateway_' + gateway['version'][1:] + '_linux_amd64.tar.gz'
subprocess.run([
    sys.executable, str(temporary / 'acquisition-gateway/adapters/testdata/acquisition-fixture/build.py'),
    '--base-archive', str(downloads / archive),
    '--base-checksums', str(downloads / 'checksums.txt'),
    '--base-provenance', str(temporary / 'acquisition-official' / 'component-artifacts.json'),
    '--base-tag', gateway['version'], '--base-commit', gateway['revision'],
    '--output', str(temporary / 'acquisition-fixture'),
], check=True)
PYTHON
}

step=${1:?usage: jobs-companion.sh STEP}
case "$step" in
  install)
    rm -rf "$official" "$tmp/jobs-downloads"
    install_official
    ;;

  runtime-jobs)
    install_official
    JPACK_RUNNER_TEST_BIN="$official/jpack-runner" JPACK_BIN="$official/jpack" \
      go test -race ./internal/desk -run 'TestJobs|TestRunnerInputProfiles|TestGraphRow1WithThePublishedRuntime|TestGraphRow3RealRuntime|TestGraphRow4PublishedRuntimeTrailUnchanged' -count=1
    ;;

  run-signer)
    install_official
    JPACK_RUNNER_TEST_BIN="$official/jpack-runner" JPACK_BIN="$official/jpack" \
      go test -race ./internal/desk -run '^TestJobsRealCompanionHoldsEachRunsSignerToTheList$' -count=1 -v | tee "$tmp/jobs-signer.log"
    need_passed "$tmp/jobs-signer.log" TestJobsRealCompanionHoldsEachRunsSignerToTheList
    ;;

  # Ownership and manual admission use the exact attested locked companions, with no candidate
  # source rebuild. Their required capabilities must be supplied by the reviewed published
  # component pins.
  executor-ownership)
    install_official
    JPACK_EXECUTOR_RUNNER_TEST_BIN="$official/jpack-runner" JPACK_BIN="$official/jpack" \
      go test -race ./internal/desk -run '^TestJobsExecutorRealEnrolledDeskRunnerRuntime$' -count=1 -v | tee "$tmp/executor-ownership.log"
    need_passed "$tmp/executor-ownership.log" TestJobsExecutorRealEnrolledDeskRunnerRuntime
    ;;

  policy-draft)
    install_official
    JPACK_POLICY_TEST_BIN="$official/jpack" \
      go test -race ./internal/desk -run '^TestPolicy(AdoptionRealRuntime(RawNumbersAndMaterialCaps|ArchiveResolvesExactPolicyRole)|CoverageRealRuntimeExactMaterialAndRefusal)$' -count=1 -v | tee "$tmp/policy-official-draft.log"
    need_passed "$tmp/policy-official-draft.log" TestPolicyAdoptionRealRuntimeRawNumbersAndMaterialCaps TestPolicyAdoptionRealRuntimeArchiveResolvesExactPolicyRole TestPolicyCoverageRealRuntimeExactMaterialAndRefusal
    ;;

  # The same Runtime checks authoring/adoption material and executes the real run. Old
  # Runner/helper evidence belongs to the Runner's CI.
  policy-manual-admission)
    install_official
    build_old_desk
    JPACK_MANUAL_RUNNER_TEST_BIN="$official/jpack-runner" \
    JPACK_MANUAL_OLD_DESK_TEST_BIN="$old_desk_bin" \
    JPACK_BIN="$official/jpack" \
      go test -race ./internal/desk -run '^TestPolicyManual(RealDeskRunnerRuntime|RealMCPRefusesOperationWithJobsUnavailable|ActualOldDeskRegistryRollbackFence)$' -count=1 -v | tee "$tmp/policy-manual-admission.log"
    need_passed "$tmp/policy-manual-admission.log" TestPolicyManualRealDeskRunnerRuntime TestPolicyManualRealDeskRunnerRuntime/registered-exact-input-and-withdrawal-first TestPolicyManualRealDeskRunnerRuntime/commit-before-withdrawal TestPolicyManualRealDeskRunnerRuntime/lost-start-reply TestPolicyManualRealDeskRunnerRuntime/lost-reserve-reply TestPolicyManualRealMCPRefusesOperationWithJobsUnavailable TestPolicyManualActualOldDeskRegistryRollbackFence
    ;;

  # The witness (ADR-0012): the gateway's own reference, run on loopback as a witness, with the
  # pinned runtime reading its chain.
  witness)
    install_official
    JPACK_BIN="$official/jpack" JPACK_GATEWAY_TEST_BIN="$official/gateway" \
      go test ./internal/desk -run '^TestWitnessWithThePublishedRuntimeAndGateway$' -count=1 -v | tee "$tmp/witness-companions.log"
    need_passed "$tmp/witness-companions.log" TestWitnessWithThePublishedRuntimeAndGateway
    ;;

  # The Go job has no runtime, so these skip there. Here they create a desk with the pinned
  # runtime, check its lock, and drive its relay: a deciding run of a draft refused, a locked
  # pack recorded, rehearsals unaffected. The review step then reviews and locks a pack with the
  # same runtime, and the upgrade offer turns an existing project's gates on.
  new-desks)
    install_official
    JPACK_BIN="$official/jpack" JPACK_TEST_BIN="$official/jpack" go test ./internal/desk -run 'TestNewDeskWithTheRuntime|TestInputRecipeRetainsBoundPackAndMappingsAcrossEdits|TestDialectAuthoring|TestReviewAndLockWithTheRuntime|TestArtifactPublication|TestArtifactVersionHistory|TestDialectPublication|TestPublishedRelease|TestPolicyRealRuntimeVersionDelegation|TestFailedCreationNeverCleansThroughSubstitutedReviewedSymlink|TestUpgradeWithTheRuntime|TestGraphRow1WithThePublishedRuntime|TestGraphRow3RealRuntime|TestGraphRow4PublishedRuntimeTrailUnchanged' -count=1 -v
    ;;

  exchange-presets)
    install_official
    JPACK_EXCHANGE_TEST_BIN="$official/jpack" go test ./internal/exchange ./internal/desk -run 'TestPreset|TestManifest|TestReleaseUpdate|TestGitRelease|TestClosedMetadata|TestLocalGit|TestExchange' -count=1 -v | tee "$tmp/exchange-import.log"
    need_passed "$tmp/exchange-import.log" TestPresetUsesRuntimeOnTwoInstallations TestExchangeReviewImportAndWithdrawal
    ;;

  dialect-gateway)
    build_dialect_gateway
    ;;

  dialect-journeys)
    install_official
    build_dialect_gateway
    JPS_TEST_GATEWAY_BUNDLE="$dialect" \
    JPACK_RUNNER_TEST_BIN="$official/jpack-runner" JPACK_BIN="$official/jpack" \
      go test -race ./internal/desk -run 'TestDialectPublication|TestPublishedRelease|TestInputSubscriptionRealRunnerOutbox|TestDialectCompanyPeopleTestedJobJourney|TestDialectProcurementTestedJobJourney|TestPublishedReviewedInputRealGatewayRunner|TestPortableResearch|TestPortableSearchBinding|TestPortablePageBinding|TestPortableScopedPage' -count=1 -v | tee "$tmp/dialect-journey.log"
    need_passed "$tmp/dialect-journey.log" TestDialectCompanyPeopleTestedJobJourney/file TestDialectCompanyPeopleTestedJobJourney/memory TestDialectProcurementTestedJobJourney/file TestDialectProcurementTestedJobJourney/memory TestPublishedReviewedInputRealGatewayRunner TestPortableResearchUsesSharedJournalAndRealReceipts/file TestPortableResearchUsesSharedJournalAndRealReceipts/memory TestPortableResearchHTTPAndSharedChatApproval TestPortableResearchImportOnTwoInstallations/first TestPortableResearchImportOnTwoInstallations/second TestPortableResearchCompanyPersonImportOnTwoInstallations/first TestPortableResearchCompanyPersonImportOnTwoInstallations/second TestPortableResearchAcrossAPIFamiliesWithRealReceipts/openai-compatible TestPortableResearchAcrossAPIFamiliesWithRealReceipts/anthropic TestPortableResearchAcrossAPIFamiliesWithRealReceipts/gemini TestPortableResearchStaleConnectionBindingRefusedBeforeAcquisition TestPortableResearchInterruptedCallKeepsCompletedAndNeverReplays/worker_cancelled TestPortableResearchInterruptedCallKeepsCompletedAndNeverReplays/stop_saved TestPortableResearchContextIsFittedAndReadFromRetainedSource TestPortableSearchBindingReviewAndImmutableRegistration TestPortablePageBindingActualCompanionReviewAndRegistration TestPortablePageBindingActualCompanionPlanAndCapacity TestPortablePageBindingActualCompanionRejectsStaleAdapterBeforeAcquisition TestPortableScopedPageRefusedBeforeAnyAcquisition TestPortableScopedPageRefusedAtItsReceiptIsNotRequestedAgain TestPortableScopedPageRefusalSurvivesRestartAndResume
    ;;

  # The same pinned Gateway verifies saved-chat documents offline. PASS is
  # required: the ordinary Go lane deliberately skips crypto without this bundle.
  saved-context)
    build_dialect_gateway
    reference_inputs=$(mktemp -d "$tmp/document-reference-inputs.XXXXXX")
    JPS_TEST_GATEWAY_BUNDLE="$dialect" JPS_DOCUMENT_REFERENCE_FIXTURES="$reference_inputs" \
      go test -race ./internal/desk -run '^TestAssistant(Document|ChatDocument)' -count=1 -v | tee "$tmp/assistant-saved-context.log"
    need_passed "$tmp/assistant-saved-context.log" \
      TestAssistantDocumentSupportedSources \
      TestAssistantDocumentTamperingAndSelection \
      TestAssistantDocumentSignedContractAndPartialConsent \
      TestAssistantDocumentUnreadablePagesAndOCR \
      TestAssistantDocumentAuthenticatedRootAndRecheck \
      TestAssistantDocumentCommandBoundsAndCancellation \
      TestAssistantChatDocumentFactory/api TestAssistantChatDocumentFactory/codex \
      TestAssistantChatDocumentSelectionsAndConsent/repeat \
      TestAssistantChatDocumentSelectionsAndConsent/pages \
      TestAssistantChatDocumentSelectionsAndConsent/consent \
      TestAssistantChatDocumentSelectionsAndConsent/partial-no-upgrade \
      TestAssistantChatDocumentSelectionsAndConsent/revision \
      TestAssistantChatDocumentUnverified \
      TestAssistantChatDocumentQueuedDispatch/api-attachment \
      TestAssistantChatDocumentQueuedDispatch/api-trust \
      TestAssistantChatDocumentQueuedDispatch/api-owner \
      TestAssistantChatDocumentQueuedDispatch/codex-attachment \
      TestAssistantChatDocumentQueuedDispatch/codex-trust \
      TestAssistantChatDocumentQueuedDispatch/codex-owner \
      TestAssistantChatDocumentContinuation/before-child \
      TestAssistantChatDocumentContinuation/before-parent
    web/node_modules/.bin/esbuild scripts/assistant-document-reference-check.mjs \
      --bundle --platform=node --format=esm --outfile="$tmp/assistant-document-reference.mjs"
    node "$tmp/assistant-document-reference.mjs" "$reference_inputs"
    ;;

  signed-exchange-release)
    install_official
    build_dialect_gateway
    exchange_release "$tmp/exchange"
    JPACK_EXCHANGE_RESEARCH_ACCEPTANCE=1 \
    JPACK_EXCHANGE_ACCEPTANCE_CATALOG="$tmp/exchange" \
    JPACK_EXCHANGE_ACCEPTANCE_TAG=v0.2.0 \
    JPACK_EXCHANGE_ACCEPTANCE_COMMIT=565ddf477c7ed986906409f19bcf8fccfaafa8a5 \
    JPACK_EXCHANGE_TEST_BIN="$official/jpack" \
    JPACK_EXCHANGE_TEST_RUNNER="$official/jpack-runner" \
    JPS_TEST_GATEWAY_BUNDLE="$dialect" \
      go test -race ./internal/desk -run '^TestExchangeSignedRelease(Import|ResearchPreparation)OnTwoInstallations$' -count=1 -v | tee "$tmp/signed-exchange.log"
    need_passed "$tmp/signed-exchange.log" TestExchangeSignedReleaseImportOnTwoInstallations TestExchangeSignedReleaseResearchPreparationOnTwoInstallations
    ;;

  # The acceptance of the signed Exchange presets against a Gateway acquisition fixture. The
  # fixture's source pin is a test transport, separate from the official component lock; its
  # binary is explicitly identified as a nonrelease program.
  acquisition-fixture)
    acq="$tmp/acquisition-official"
    rm -rf "$acq" "$tmp/acquisition-downloads" "$tmp/acquisition-fixture"
    python3 "$root/scripts/dev/official-companions.py" "$acq" "$tmp/acquisition-downloads"
    exchange_release "$tmp/acquisition-exchange"
    checkout_at "$tmp/acquisition-gateway" judgment-pack-gateway 4b9fa82ddeae772654d8f8bcf7b33d47ac65a512 full
    git -C "$tmp/acquisition-gateway" config gpg.ssh.allowedSignersFile "$root/.github/testdata/gateway-release-allowed-signers"
    GOWORK=off go -C "$tmp/acquisition-gateway/adapters" mod download
    build_acquisition_fixture
    python3 "$tmp/acquisition-gateway/adapters/testdata/acquisition-fixture/smoke.py" --bundle "$tmp/acquisition-fixture"
    ;;

  acquisition-acceptance)
    acq="$tmp/acquisition-official"
    [ -f "$acq/component-artifacts.json" ] && [ -d "$tmp/acquisition-fixture" ] && [ -d "$tmp/acquisition-exchange/.git" ] || bash "$0" acquisition-fixture
    JPACK_EXCHANGE_ACQUISITION_ACCEPTANCE=1 \
    JPACK_EXCHANGE_ACCEPTANCE_CATALOG="$tmp/acquisition-exchange" \
    JPACK_EXCHANGE_ACCEPTANCE_TAG=v0.2.0 \
    JPACK_EXCHANGE_ACCEPTANCE_COMMIT=565ddf477c7ed986906409f19bcf8fccfaafa8a5 \
    JPACK_EXCHANGE_TEST_BIN="$acq/jpack" \
    JPACK_EXCHANGE_TEST_RUNNER="$acq/jpack-runner" \
    JPACK_EXCHANGE_COMPONENT_PROVENANCE="$acq/component-artifacts.json" \
    JPS_TEST_GATEWAY_BUNDLE="$tmp/acquisition-fixture" \
      go test -race ./internal/desk -run '^TestExchange(SignedReleaseAcquisitionsOnTwoInstallations|AcquisitionScopeAndRedirectRefusals)$' -count=1 -v | tee "$tmp/exchange-acquisition.log"
    names=(TestExchangeSignedReleaseAcquisitionsOnTwoInstallations TestExchangeAcquisitionScopeAndRedirectRefusals
           TestExchangeAcquisitionScopeAndRedirectRefusals/before_acquisition TestExchangeAcquisitionScopeAndRedirectRefusals/final_redirect)
    for installation in first second; do
      for preset in judgmentpack.company-research judgmentpack.people-at-company judgmentpack.procurement-opportunity; do
        names+=("TestExchangeSignedReleaseAcquisitionsOnTwoInstallations/$installation/$preset")
      done
    done
    need_passed "$tmp/exchange-acquisition.log" "${names[@]}"
    ;;

  *)
    echo "jobs-companion.sh: no step named $step" >&2
    exit 2
    ;;
esac
