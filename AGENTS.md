# Volcano Developer Guide for AI Coding Agents

This guide provides essential information for AI coding agents working on the Volcano codebase.

## Project Overview

Volcano is a Kubernetes-native batch scheduling system written in Go 1.24+. It extends kube-scheduler for AI/ML, Big Data, and HPC workloads. The project follows standard Go conventions with Kubernetes-specific patterns.

## Build Commands

### Core Binaries
```bash
# Build all components
make all

# Build individual components
make vc-scheduler              # Scheduler
make vc-controller-manager     # Controller manager
make vc-webhook-manager        # Webhook admission controller
make vc-agent                  # Agent and network-qos
make vcctl                     # CLI tool

# Build command-line utilities
make command-lines             # vcancel, vresume, vsuspend, vjobs, vqueues, vsub

# Clean build artifacts
make clean
```

### Docker Images
```bash
# Build all images
make images

# Build individual component images
make vc-scheduler-image
make vc-controller-manager-image
make vc-webhook-manager-image
make vc-agent-image
```

## Testing Commands

### Unit Tests
```bash
# Run all unit tests
make unit-test

# Run tests for specific package
go test -v volcano.sh/volcano/pkg/scheduler/cache

# Run single test function
go test -v volcano.sh/volcano/pkg/scheduler/cache -run TestGetOrCreateJob

# Run with race detector
go test -race volcano.sh/volcano/pkg/...

# Clean test cache before running
go clean -testcache
```

### E2E Tests
```bash
# Run all e2e tests
make e2e

# Run specific e2e test suites
make e2e-test-schedulingbase
make e2e-test-schedulingaction
make e2e-test-jobp
make e2e-test-jobseq
make e2e-test-vcctl
make e2e-test-stress
make e2e-test-cronjob
make e2e-test-admission-webhook
```

### Linting and Verification
```bash
# Run golangci-lint
make lint

# Verify formatting and generated code
make verify

# Verify generated YAML files
make verify-generated-yaml

# Check licenses
make lint-licenses
```

## Code Style Guidelines

### Import Organization

Use **3-section imports** with blank lines between:

```go
import (
    // Section 1: Standard library (alphabetically sorted)
    "context"
    "fmt"
    "time"

    // Section 2: Third-party packages (k8s.io, github.com, etc.)
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"

    // Section 3: Local volcano.sh packages
    batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
    "volcano.sh/volcano/pkg/scheduler/api"
)
```

**Import prefix:** Use `goimports` with `local-prefixes: volcano.sh` (configured in .golangci.yml)

### Naming Conventions

- **Packages:** Short, lowercase, single word (e.g., `cache`, `scheduler`, `util`)
- **Exported types:** PascalCase (e.g., `SchedulerCache`, `NodeInfo`, `TaskInfo`)
- **Unexported types:** camelCase (e.g., `defaultEvictor`, `defaultBinder`)
- **Interfaces:** Descriptive names, often ending in -er (e.g., `Binder`, `Evictor`, `StatusUpdater`)
- **Functions:** PascalCase (exported) or camelCase (unexported), verb-noun pattern (e.g., `NewNodeInfo`, `AddTask`, `allocateIdleResource`)
- **Variables:** Short names in local scope (e.g., `pg`, `sc`, `ni`, `err`, `ctx`), descriptive names at package level
- **Test functions:** `Test<TypeName>_<MethodName>_<Scenario>` or `Test<FunctionName>`

### Error Handling

**Pattern A: Immediate return with context**
```go
if err != nil {
    return fmt.Errorf("failed to find Job %v for Task %v", jobID, taskID)
}
```

**Pattern B: Logging with klog**
```go
if err != nil {
    klog.Errorf("Failed to update pod <%v/%v> status: %v", pod.Namespace, pod.Name, err)
    return err
}
```

**Pattern C: Error accumulation**
```go
if len(errs) != 0 {
    return fmt.Errorf("failed to kill %d pods of %d", len(errs), total)
}
```

**Guidelines:**
- Use `fmt.Errorf` for error context (no error wrapping packages)
- Use `klog` for structured logging with verbosity levels (V(3), V(4), etc.)
- Check errors immediately: `if err != nil`
- Never ignore errors without explicit justification

### Comments and Documentation

**File headers:** Apache 2.0 license (see existing files)

**Function documentation:** GoDoc style (starts with function name)
```go
// New returns a Cache implementation that manages scheduler state.
func New(config *rest.Config, ...) Cache { ... }

// AddTask adds a task to the node and updates resource allocation.
func (ni *NodeInfo) AddTask(task *TaskInfo) error { ... }
```

**Struct documentation:**
```go
// NodeInfo is node-level aggregated information including resources,
// state, and scheduled tasks.
type NodeInfo struct {
    Name  string
    State NodeState
    // The releasing resource on that node
    Releasing *Resource
}
```

**Inline comments:** Explain "why" not "what", use sparingly for complex logic

### Formatting

- **Indentation:** Tabs (not spaces) - enforced by `gofmt`
- **Line length:** No strict limit, but be reasonable (typically < 120 characters)
- **Blank lines:** Separate logical blocks, one blank line between functions
- **Whitespace:** Use `whitespace` linter settings (no trailing whitespace)

### Test Patterns

**Table-driven tests:**
```go
func TestKillJob(t *testing.T) {
    testcases := []struct {
        Name      string
        Job       *v1alpha1.Job
        ExpectVal error
    }{
        {
            Name: "KillJob success case",
            Job:  &v1alpha1.Job{...},
            ExpectVal: nil,
        },
    }
    for _, tc := range testcases {
        t.Run(tc.Name, func(t *testing.T) {
            // test logic
        })
    }
}
```

**E2E tests:** Use Ginkgo/Gomega framework
```go
var _ = Describe("Job E2E Test", func() {
    It("should run job successfully", func() {
        Expect(err).NotTo(HaveOccurred())
    })
})
```

**Test helpers:** Use `build*` prefix for test object creation

## Code Generation

```bash
# Generate code (CRDs, clients, informers, listers)
make generate-code

# Generate CRD manifests
make manifests

# Generate YAML files
make generate-yaml

# Generate Helm charts
make generate-charts
```

## Commit Message Format

Follow this convention:
```
<subsystem>: <what changed>

<why this change was made>

Fixes #<issue-number>
```

Example:
```
scheduler: add gang scheduling support for Ray jobs

This enables Ray jobs to use gang scheduling to ensure all workers
start together, improving resource utilization and reducing deadlocks.

Fixes #1234
```

**Guidelines:**
- Subject line ≤ 70 characters
- Body wrapped at 80 characters
- Focus on "why" not "what"

## Linter Configuration

Enabled linters (`.golangci.yml`):
- `gofmt`, `goimports`, `govet` - Go standard checks
- `staticcheck`, `gosimple`, `ineffassign` - Code quality
- `typecheck`, `unused` - Type safety and dead code
- `depguard` - Dependency restrictions (no `k8s.io/klog` v1, no `io/ioutil`)
- `whitespace` - Formatting

**Deprecated packages to avoid:**
- `k8s.io/klog` → use `k8s.io/klog/v2`
- `io/ioutil` → use `io` and `os` packages (Go 1.16+)

## Key Directories

- `cmd/` - Main applications (scheduler, controller-manager, webhook-manager, agent, cli)
- `pkg/` - Core libraries (scheduler, controllers, webhooks)
- `test/e2e/` - End-to-end tests
- `hack/` - Build and development scripts
- `installer/` - Deployment manifests and Dockerfiles
- `config/` - CRD definitions
- `staging/src/volcano.sh/apis/` - API definitions (local development)

## Additional Resources

- [Contributing Guide](contribute.md)
- [Development Setup](docs/development/prepare-for-development.md)
- [Build Instructions](docs/development/development.md)
