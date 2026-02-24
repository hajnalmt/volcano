# Tilt Dev Loop (Kind kind-volcano-dev)

Use this skill to run and operate Volcano's local Tilt development loop on the
Kind cluster named `kind-volcano-dev`, provisioned by `ctlptl` with a local registry.

## When to use

- Start or stop local Tilt-driven development.
- Check resource health quickly.
- Fetch logs for failing resources.
- Trigger targeted rebuilds after code changes.
- Wait for the environment to become ready before tests.

## Ground rules

- Use repository root as the working directory.
- Always use Makefile wrappers (`make dev-*`) over raw `_output/bin/tilt` commands.
- Keep cluster name fixed to `kind-volcano-dev`.
- Use `hack/tilt/Tiltfile` as the single Tilt entrypoint.

## Canonical workflow

1) Start environment

```bash
make dev-up
```

For long-running sessions, prefer daemon mode from the Makefile so logs do not
stream in the CLI:

```bash
nohup make dev-up > /tmp/dev-up.log 2>&1 &
```

What this does:
- Ensures `tilt`, `kind`, and `ctlptl` binaries are available in `_output/bin/`.
- Creates/updates a `ctlptl`-managed Kind cluster + local registry from `hack/tilt/ctlptl-kind-registry.yaml`.
- Runs `tilt up -f hack/tilt/Tiltfile`.

2) Quick status snapshot

```bash
make dev-tilt-status
```

3) Wait for resources to settle

```bash
make dev-wait-ready
```

Always run this after `make dev-up` before triggering tests.

4) Inspect one resource deeply

```bash
make dev-tilt-describe RESOURCE=volcano-scheduler
```

5) Tail logs

All resources:

```bash
make dev-tilt-logs
```

Single resource:

```bash
make dev-tilt-logs RESOURCE=volcano-scheduler
```

Log hygiene for background mode:

```bash
# clear previous daemon log before a fresh start
: > /tmp/dev-up.log
```

6) Force rebuild/redeploy for one resource

```bash
make dev-tilt-trigger RESOURCE=volcano-scheduler
```

7) Stop environment

```bash
make dev-down
```

8) Full cleanup (Tilt down + delete Kind cluster + remove local binaries)

```bash
make dev-clean
```

## Troubleshooting playbook

If `make dev-up` fails:

1. Confirm cluster exists:

```bash
_output/bin/ctlptl get cluster
```

Expected: a cluster named `kind-volcano-dev` appears in the list.

2. Confirm context matches Tiltfile expectation:

```bash
kubectl config get-contexts -o name | grep -E '^(kind-volcano-dev|kind-kind-volcano-dev)$'
```

3. Verify Tilt can see resources:

```bash
make dev-tilt-status
```

4. If a resource is stuck/unhealthy, gather details and logs:

```bash
make dev-tilt-describe RESOURCE=<resource-name>
make dev-tilt-logs RESOURCE=<resource-name>
```

5. Retry only the affected resource:

```bash
make dev-tilt-trigger RESOURCE=<resource-name>
```

If an e2e test resource is stuck in `Pending` or `UpdatePending` state, it means
a dependency (e.g., `install-ginkgo`) has not been built yet. Dependencies only
need to be triggered once after Tilt comes up — subsequent test triggers reuse
the already-built dependency.

Correct flow:

1. Trigger the e2e test resource.
2. Check its state with `make dev-tilt-describe RESOURCE=<test-resource>`.
3. If it shows `Pending` / `UpdatePending`, trigger the missing dependency.
4. **Do NOT re-trigger the test resource** — Tilt already has it queued and will
   run it automatically once the dependency becomes ready.
5. Just `make dev-wait-ready TILT_WAIT_RESOURCES=uiresource/<test-resource>`.

Example:

```bash
# 1. Trigger the test
make dev-tilt-trigger RESOURCE=e2e-tests-schedulingbase-focused-example

# 2. Check state — if Pending, trigger the dependency
make dev-tilt-trigger RESOURCE=install-ginkgo

# 3. Wait — Tilt runs the test automatically after the dependency completes
make dev-wait-ready TILT_WAIT_RESOURCES=uiresource/e2e-tests-schedulingbase-focused-example
```

## CI-style run

Use non-interactive mode when a bounded run is needed:

```bash
_output/bin/tilt ci -f hack/tilt/Tiltfile
```

## Notes for agentic usage

- **Prefer use Makefile wrappers** over raw `_output/bin/tilt` commands:
  `make dev-tilt-status`, `make dev-tilt-describe`, `make dev-tilt-logs`,
  `make dev-tilt-trigger`, `make dev-wait-ready`, `make dev-up`, `make dev-down`.
- Keep commands deterministic and resource-scoped when possible.
- Collect status first, then logs, then trigger retries.
- Treat `make dev-up` as idempotent for local loops.
- First `make dev-up` run takes ~20-25 minutes (Go image compilation). Subsequent
  runs reuse Docker layer cache and are much faster.
- To avoid full image rebuilds when switching branches, start Tilt on `master`
  first, then `git checkout` to the feature branch. Tilt will perform an
  incremental live-update instead of a full rebuild.
- Use `make dev-down` (keeps cluster) for quick restarts. Use `make dev-clean`
  only for full teardown.
