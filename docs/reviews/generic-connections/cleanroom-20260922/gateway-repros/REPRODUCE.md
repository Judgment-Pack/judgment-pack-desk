# Independent gateway review probes

All added files and generated records are confined to this directory. Reviewed source trees were not edited. These probes use only local fixtures and the published corpus test signing seed.

Run from `/tmp/jp-foundation-cleanroom-20260922/gateway/adapters`:

```sh
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go run /tmp/jp-foundation-cleanroom-20260922/gateway-repros/producer_probe.go
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go run ./cmd/gateway-connections --catalog-v3 > /tmp/jp-foundation-cleanroom-20260922/gateway-repros/catalog.json
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go run ./cmd/gateway-connections --local-plan > /tmp/jp-foundation-cleanroom-20260922/gateway-repros/plan.json
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go test ./...
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go vet ./...
```

The full adapter suite requires permission to open local loopback listeners.

Run from this directory:

```sh
/home/onword/.nvm/versions/node/v22.23.1/bin/node --import /tmp/jp-foundation-cleanroom-20260922/desk/web/node_modules/tsx/dist/loader.mjs /tmp/jp-foundation-cleanroom-20260922/gateway-repros/consumer_probe.mts
/home/onword/.nvm/versions/node/v22.23.1/bin/node /tmp/jp-foundation-cleanroom-20260922/desk/web/node_modules/vitest/vitest.mjs run --config /tmp/jp-foundation-cleanroom-20260922/gateway-repros/vitest.config.mjs
python - <<'PY'
import json
from pathlib import Path
from jsonschema import Draft202012Validator
root = Path('/tmp/jp-foundation-cleanroom-20260922')
validator = Draft202012Validator(json.loads((root/'gateway/testdata/attachments/attachment-v1.schema.json').read_text()))
for path in sorted((root/'gateway-repros/records').glob('*.json')):
    validator.validate(json.loads(path.read_text()))
    print('PASS schema', path.name)
PY
```

Run from `/tmp/jp-foundation-cleanroom-20260922/gateway/go`:

```sh
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go run . conform
```

Run from `/tmp/jp-foundation-cleanroom-20260922/desk` with local test listeners enabled:

```sh
GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go test ./internal/desk -run 'Test(ConnectionCatalog|LocalPlan|IndependentGateway)' -count=1
```

`producer-probe.log` and `consumer-probe.log` preserve probe output. The original selected Go test attempts failed because the filesystem sandbox also blocked test listeners. Reruns with local-listener permission passed. The first Node invocation used the shell's old Node and rejected `--import`; subsequent commands explicitly used Node 22.23.1. Catalog tests use Vitest because direct Node execution does not implement Vite's `import.meta.glob`.
