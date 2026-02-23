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
- Prefer project binaries from `_output/bin/` when available.
- Keep cluster name fixed to `kind-volcano-dev`.
- Use `hack/tilt/Tiltfile` as the single Tilt entrypoint.

## Canonical workflow

1) Start environment

```bash
make dev-up
```

What this does:
- Ensures `tilt`, `kind`, and `ctlptl` binaries are available in `_output/bin/`.
- Creates/updates a `ctlptl`-managed Kind cluster + local registry from `hack/tilt/ctlptl-kind-registry.yaml`.
- Runs `tilt up -f hack/tilt/Tiltfile`.

2) Quick status snapshot

```bash
_output/bin/tilt get uiresource
```

3) Wait for resources to settle

```bash
make dev-wait-ready
```

4) Inspect one resource deeply

```bash
_output/bin/tilt describe uiresource volcano-scheduler
```

5) Tail logs

All resources:

```bash
_output/bin/tilt logs
```

Single resource:

```bash
_output/bin/tilt logs volcano-scheduler
```

6) Force rebuild/redeploy for one resource

```bash
_output/bin/tilt trigger volcano-scheduler
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
_output/bin/tilt get uiresource
```

4. If a resource is stuck/unhealthy, gather details and logs:

```bash
_output/bin/tilt describe uiresource <resource-name>
_output/bin/tilt logs <resource-name>
```

5. Retry only the affected resource:

```bash
_output/bin/tilt trigger <resource-name>
```

## CI-style run

Use non-interactive mode when a bounded run is needed:

```bash
_output/bin/tilt ci -f hack/tilt/Tiltfile
```

## Notes for agentic usage

- Prefer `tilt get/describe/logs/trigger/wait` over parsing UI output.
- Keep commands deterministic and resource-scoped when possible.
- Collect status first, then logs, then trigger retries.
- Treat `make dev-up` as idempotent for local loops.
