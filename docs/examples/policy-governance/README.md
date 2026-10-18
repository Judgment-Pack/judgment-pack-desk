# Synthetic policy governance interface examples

These examples accompany the [interface proposal](../../design/policy-governance-interfaces.md),
revision **0.0.1**. They are design fixtures, not adopted JPS companion documents,
service responses, operational policies or evidence that any service ran.

| Example | Policy clause | Typed fact supplied by a separately reviewed Dialect |
| --- | --- | --- |
| [Data access](data-access.json) | Automated release requires a de-identified dataset | `/dataset/deidentified` |
| [Procurement](procurement.json) | Automated acceptance requires a documented competitive procedure | `/procurement/competitiveProcedureDocumented` |
| [Refunds](refunds.json) | Automated acceptance requires eligibility within the published return window | `/purchase/withinReturnWindow` |

All three use the same interface shapes. Each narrow example has one clause:
documented true produces an eligible outcome, documented false produces an
ineligible outcome, and missing facts require human review. The proposed policy
document preserves the clause; the pack contains the executable interpretation.
In a real deployment, the Dialect must establish these typed facts from reviewed
sources. The sample does not certify that classification or preparation process.

## Reading a fixture

1. `material` retains a source, proposed Policy candidate, illustrative pack,
   review workflow, interpretation basis, adoption, release binding and exact
   synthetic input/action payloads.
2. `sourceObservation` reports the retained source. This synthetic provider has
   no external approval lifecycle; Desk-managed review supplies publication.
3. `reviewRequest` and `commands` show exact source, content and workflow bindings.
   A section reviewer signs first; a whole-policy approver signs afterward.
4. `modeledResponses.publication` records a fictional publication at 12:04 UTC,
   scheduled effective at 12:05. A run is admitted at 12:06. The policy is
   withdrawn at 12:07; the run's later action is blocked at 12:08.
5. Recovery returns the original publication/admission receipt. Recovering an
   admission receipt does **not** dispatch an operation again.

`servicePrerequisites` explicitly marks fictional pre-established membership,
adoption approval, release registration and action authorization. A source
observation, an imported fixture or a caller-supplied `approved` value cannot
establish those prerequisites. These payloads cannot be sent to an implemented
API: operation names and the proposed release binding still require review.

The workflow shape is an illustrative two-stage configuration, not an adopted
workflow language. Pack-shaped examples name the existing `0.2.0-draft` Core
format, but were not executed or validated by Runtime for this design exercise.
No application source, API or advertised capability is added here.

## Reproducing identities

The outer fixture JSON is a readable envelope; its whole-file hash is not a
referenced artifact identity. Each `material` entry defines exact retained bytes:

- `encoding: utf8`: UTF-8 bytes of `text`, including its final newline.
- `encoding: canonical-json`: the proposed `canon` representation of `document`,
  with no trailing newline. These fixtures use only safe integers, booleans,
  strings, arrays, objects and null.

`content.bytesDigest` and `byteLength` describe those bytes. Documents happen to
be retained in canonical form here, so their raw-byte and canonical digests are
equal. Production readers must not assume that equality for arbitrary pack bytes.
Request digests use the same proposed canonical representation of the exact
command body. Resolver access control remains necessary even with a valid digest.

From the repository root:

```sh
python3 docs/examples/policy-governance/verify.py
```

The helper checks fixture JSON, retained-byte identities, reference resolution,
review/consumer/dependency bindings, modeled command receipts and recovery
identity. It only handles this example envelope. It is **not** a Policy validator,
a Runtime substitute, an authentication check or an implementation of admission.
The concurrency, live-provider, restoration and cross-installation cases in the
interface proposal still need actual integration tests.
