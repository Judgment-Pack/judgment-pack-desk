"""Check the synthetic example envelope, never live governance or conformance."""

import hashlib
import json
import re
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"Duplicate JSON key: {key}")
        result[key] = value
    return result


def validate_domain(value):
    if isinstance(value, dict):
        for key, child in value.items():
            key.encode("utf-8", errors="strict")
            validate_domain(child)
    elif isinstance(value, list):
        for child in value:
            validate_domain(child)
    elif isinstance(value, str):
        value.encode("utf-8", errors="strict")
    elif value is None or isinstance(value, bool):
        pass
    elif isinstance(value, int):
        require(abs(value) <= 2**53 - 1, "Integer outside proposed canon domain")
    else:
        raise ValueError(f"Unsupported value in example: {type(value).__name__}")


def canonical(value):
    validate_domain(value)
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")


def digest(value):
    return "sha256:" + hashlib.sha256(value).hexdigest()


def walk(value):
    yield value
    if isinstance(value, dict):
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def verify(path):
    fixture = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    validate_domain(fixture)
    require(fixture["synthetic"] is True and fixture["proposalVersion"] == "0.0.1", "Not a proposal fixture")
    material = fixture["material"]
    retained = {}
    for name, entry in material.items():
        if entry["encoding"] == "utf8":
            body = entry["text"].encode("utf-8")
        else:
            require(entry["encoding"] == "canonical-json", f"Unknown encoding: {name}")
            body = canonical(entry["document"])
        content = entry["content"]
        require(content["bytesDigest"] == digest(body), f"Digest mismatch: {name}")
        require(content["byteLength"] == len(body), f"Size mismatch: {name}")
        require(content["mediaType"] == ("text/plain" if entry["encoding"] == "utf8" else "application/json"), f"Media type mismatch: {name}")
        retained[content["bytesDigest"]] = content
    for node in walk(fixture):
        if isinstance(node, dict) and "bytesDigest" in node:
            require(node == retained.get(node["bytesDigest"]), "Unresolved or altered content reference")

    def doc(name):
        return material[name]["document"]

    def content(name):
        return material[name]["content"]

    def check_artifact(reference, name):
        artifact = doc(name)
        require(reference["id"] == artifact["id"] and reference["version"] == artifact["version"], "Artifact identity mismatch")
        require(re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", reference["version"]), "Invalid example artifact version")
        require(reference["content"] == content(name), "Artifact content mismatch")

    adoption = doc("adoption")
    release = doc("release-binding")
    check_artifact(adoption["consumer"], "consumer-pack")
    require(release["consumer"] == adoption["consumer"], "Release consumer mismatch")
    require(release["adoption"] == content("adoption"), "Release adoption mismatch")
    require(release["dependencies"] == [x["policy"] for x in adoption["governingPolicies"]], "Missing or substituted governing dependency")
    for governing in adoption["governingPolicies"]:
        check_artifact(governing["policy"], "policy-candidate")
        sections = {x["id"] for x in doc("policy-candidate")["sections"]}
        elements = {x["id"] for x in doc("consumer-pack")["rules"]}
        for coverage in governing["coverage"]:
            require(coverage["sectionId"] in sections, "Unresolved section")
            require(set(coverage["consumerElementIds"]).issubset(elements), "Unresolved consumer element")
            require(coverage["reviewBasis"] == content("interpretation-review-basis"), "Wrong interpretation basis")

    review = fixture["reviewRequest"]
    check_artifact(review["candidate"], "policy-candidate")
    require(review["workflow"]["content"] == content("review-workflow"), "Workflow mismatch")
    require(review["workflow"]["revision"] == doc("review-workflow")["revision"], "Workflow revision mismatch")
    require(review["sourceBasis"][0]["observationId"] == fixture["sourceObservation"]["observationId"], "Source basis mismatch")
    require(review["sourceBasis"][0]["content"] == content("source"), "Source basis content mismatch")
    require(fixture["sourceObservation"]["content"] == content("source"), "Source observation mismatch")
    commands, responses = fixture["commands"], fixture["modeledResponses"]
    require(len(commands["submitSignoffs"]) == len(responses["signoffs"]), "Sign-off receipt count mismatch")
    for command, receipt in zip(commands["submitSignoffs"], responses["signoffs"]):
        require(receipt["requestDigest"] == digest(canonical(command)), "Sign-off request mismatch")
        require(receipt["candidateDigest"] == content("policy-candidate")["bytesDigest"], "Sign-off candidate mismatch")
        require(receipt["workflowDigest"] == content("review-workflow")["bytesDigest"], "Sign-off workflow mismatch")
    for command_key, response_key in [("publishPolicy", "publication"), ("commitRunStart", "runAdmission"), ("withdrawPolicy", "withdrawal"), ("commitActionStart", "actionAdmission")]:
        receipt = responses[response_key]["receipt"]
        require(receipt["requestDigest"] == digest(canonical(commands[command_key])), f"Command receipt mismatch: {command_key}")
        require(receipt["operationId"] == commands[command_key]["operationId"], "Operation identity mismatch")
    for key in ["publication", "runAdmission"]:
        recovery = responses[key + "Recovery"]
        require(recovery["delivery"] == "recovered" and recovery["receipt"] == responses[key]["receipt"], "Recovery changed the receipt")
    check_artifact(responses["publication"]["receipt"]["policy"], "policy-candidate")
    require(responses["publication"]["receipt"]["signoffReceiptIds"] == [item["receiptId"] for item in responses["signoffs"]], "Publication sign-off references mismatch")
    for command_key, payload in [("commitRunStart", "run-payload"), ("commitActionStart", "action-payload")]:
        require(commands[command_key]["releaseEnvelopeDigest"] == content("release-binding")["bytesDigest"], "Start release mismatch")
        require(commands[command_key]["payloadDigest"] == content(payload)["bytesDigest"], "Start payload mismatch")
    for command_key, response_key in [("commitRunStart", "runAdmission"), ("commitActionStart", "actionAdmission")]:
        for field in ["attemptId", "executorId", "executionEpoch", "releaseEnvelopeDigest", "payloadDigest"]:
            require(responses[response_key]["receipt"][field] == commands[command_key][field], f"Admission receipt binding mismatch: {field}")
    require(responses["runAdmission"]["receipt"]["decision"] == "start-committed", "Expected modeled run admission")
    require(responses["actionAdmission"]["receipt"]["decision"] == "blocked", "Expected modeled post-withdrawal refusal")
    return len(material)


if __name__ == "__main__":
    paths = sorted(Path(__file__).parent.glob("*.json"))
    require(len(paths) == 3, "Expected the three documented domain examples")
    count = sum(verify(path) for path in paths)
    print(f"Verified {len(paths)} synthetic fixtures and {count} retained byte identities; no live service or Runtime conformance checked.")
