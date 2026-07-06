## Who This Is For
- **AI agents**: Automate repository tasks with minimal context
- **Contributors**: Humans using AI assistants or working directly
- **Maintainers**: Ensure assistants follow project conventions and CI rules

## About This Repository
This repository is based on **v1.9.0** of [kubeflow/trainer](https://github.com/kubeflow/trainer)
and uses the **v1 API** (`kubeflow.org/v1`) with per-framework job kinds:
PyTorchJob, TFJob, XGBoostJob, MPIJob, PaddleJob, JAXJob.

The Go module path is `github.com/kubeflow/training-operator`, all Go imports must use this path, even though the repo is hosted under opendatahub-io.

## Branch Strategy
| Branch | Purpose | Sync |
|--------|---------|------|
| `dev` | **Default**. Active development, PRs land here | — |
| `stable` | Verified builds for ODH | Auto-synced from `dev` every 4 hours (`sync-dev-to-stable.yml`) |
| `rhoai` | RHOAI release branch | Auto-synced from `stable` every 4 hours (`sync-stable-to-rhoai.yml`) |

**⚠️ NEVER push directly to `stable` or `rhoai` — always go through the gated sync process.**

Sync PRs use gated labels: `lake-gate` (dev→stable) and `ocean-gate` (stable→rhoai).
Both require manual E2E verification before merge. To merge, an authorized user (listed in
`OWNERS_ALIASES` under `approve-lake-gate` or `approve-ocean-gate`) comments `/approve` on the PR.
This triggers a **fast-forward merge** via `approve-lake-gate.yml` / `approve-ocean-gate.yml` —
do NOT use the GitHub merge button; it won't work correctly for these PRs.

## Repository Layout
```
opendatahub-io/training-operator/
├── .github/                          # GitHub Actions CI/CD
├── .tekton/                          # Konflux/Tekton CI pipelines
├── build/images/training-operator/   # Container images
│   ├── Dockerfile                    # Standard multi-stage (Go → distroless)
│   ├── Dockerfile.rhoai              # UBI9, FIPS-compliant (ODH/RHOAI)
│   └── Dockerfile.multiarch          # Multi-arch builds
├── cmd/training-operator.v1/         # Single binary entrypoint
│   └── main.go                       # controller-runtime manager setup
├── docs/                             # Proposals and API reference
├── examples/                         # Sample training jobs per framework
├── hack/                             # Code generation and build scripts
│   ├── update-codegen.sh             # k8s code-generator (clients, informers, listers)
│   ├── verify-codegen.sh             # Verify generated code is up to date
│   ├── generate-apidoc.sh            # AsciiDoc API docs
│   ├── python-sdk/                   # OpenAPI Generator for Python SDK
│   └── boilerplate/                  # License headers for generated files
├── manifests/                        # Kustomize deployment manifests
│   ├── base/                         # CRDs, RBAC, webhook, deployment
│   ├── overlays/                     # Overlay variants (e.g. standalone)
│   ├── rhoai/                        # ODH/RHOAI-specific overlay
│   └── component_metadata.yaml       # ⚠️ Must update version on rebases
├── odh_utils/                        # ODH/RHOAI release tooling
│   ├── update-training.sh            # Script to update operator on cluster
│   └── csv-patch.json                # OLM CSV patch
├── pkg/                              # Core Go packages
│   ├── apis/kubeflow.org/v1/         # CRD type definitions (6 job kinds)
│   ├── controller.v1/                # Kubernetes controllers (one subdir per job kind)
│   │   ├── common/                   # Shared JobController (pod/service lifecycle)
│   │   └── register_controller.go    # Controller registration
│   ├── webhooks/                     # Validating webhooks per job type
│   ├── client/                       # Generated clientset, informers, listers
│   ├── cert/                         # Webhook TLS cert management
│   ├── config/                       # Runtime configuration
│   ├── common/                       # Shared interfaces and metrics
│   ├── core/                         # Pod/service/job core helpers
│   └── util/                         # Utility functions
├── sdk/python/                       # Python SDK (kubeflow-training on PyPI)
│   ├── kubeflow/training/            # Generated OpenAPI client + high-level API
│   ├── kubeflow/trainer/             # Trainer image for fine-tuning
│   ├── kubeflow/storage_initializer/ # Model storage initializer
│   └── test/                         # E2E and integration tests (pytest)
│       ├── e2e/                      # Per-framework e2e tests
│       └── e2e-fine-tune-llm/        # LLM fine-tuning e2e
├── scripts/                          # CI and local setup scripts
└── test_job/                         # Reference scaffold for adding new controllers
```

## Environment & Tooling
- **Go**: Primary language for controller, APIs, webhooks
- **Python**: SDK client, E2E tests, storage initializer, trainer
- **Build**: `make` (orchestration), `go build`, `docker`
- **Lint/format**: `golangci-lint`, `gofmt` (Go); `black`, `isort`, `flake8` (Python)
- **Tests**: `go test` + envtest (unit), pytest (Python SDK + E2E), Kind clusters (integration)
- **Code generation**: `controller-gen`, k8s `code-generator`, OpenAPI Generator
- **Pre-commit**: Config provided (`.pre-commit-config.yaml`) and enforced in CI

## Commands

### Build
```bash
make build                                        # Build bin/manager binary
docker build -t training-operator:test -f build/images/training-operator/Dockerfile .
docker build -t training-operator:test -f build/images/training-operator/Dockerfile.rhoai .   # RHOAI/FIPS
```

### Testing
```bash
make test                                         # Go unit tests with envtest (K8s 1.31 default)
make testall                                      # Full suite: generate + fmt + vet + lint + test

# Targeted tests
go test ./pkg/controller.v1/...                   # All controller tests
go test -v -run TestElasticGenerate ./pkg/controller.v1/pytorch/  # Specific test function

# Python SDK unit test
pytest ./sdk/python/kubeflow/training/api/training_client_test.py

# E2E (requires Kind cluster — run via CI or local Kind setup)
pytest ./sdk/python/test/e2e/
```

### Lint/format & pre-commit
```bash
make fmt                                          # Format all Go code
make vet                                          # Vet all Go code
make golangci-lint                                # Run golangci-lint project-wide
pre-commit run --all-files                        # Run all pre-commit hooks (install first: pre-commit install)
```

### Targeted lint/format
For quick feedback on specific files or packages instead of running project-wide `make` targets:

```bash
# Go
make golangci-lint LINT_PKG=./pkg/controller.v1/...       # Lint a single Go package
go vet ./pkg/controller.v1/...                            # Vet a single Go package
gofmt -w path/to/file.go                                  # Format a single Go file

# Python (uses pinned versions from .pre-commit-config.yaml)
pre-commit run flake8 --files path/to/file.py             # Lint a single Python file
pre-commit run black --files path/to/file.py              # Format a single Python file
pre-commit run isort --files path/to/file.py              # Sort imports in a single Python file
```

### Code generation (always run after modifying `pkg/apis/`)
```bash
make generate                                     # Full codegen (multi-step, see Makefile)
make manifests                                    # Only CRDs, RBAC, webhook configs (subset of generate)
```

## Agent Behavior Rules
- Make atomic, minimal changes. Keep diffs small and scoped.
- NEVER modify `.tekton/`, sync workflows, or `odh_utils/` unless explicitly asked.
- NEVER modify generated code under `pkg/client/` or `sdk/python/kubeflow/training/models/` by hand — run `make generate`.
- After changing CRD types in `pkg/apis/kubeflow.org/v1/`, always run `make generate`.
- Match import patterns and error-handling style from neighboring files.
- Use structured logging: `ctrl.Log.WithName(...)` (match existing controller patterns).
- Preserve label conventions: `training.kubeflow.org/*` prefix for all operator labels.
- Security scanning (`semgrep.yaml` + `.gitleaks.toml`) is enforced — do not introduce secrets or known vulnerability patterns.

## Commit/PR Conventions
- Include "why" in commit messages, not just "what"
- Do not commit secrets, generated files, or binary artifacts
