# CLAUDE.md — terraform-provider-idcloudhost

> Operational context for Claude Code. This is the "how to work here" file.

## What this repo is

A **custom, project-scoped Terraform provider for IDCloudHost**, written in Go using the
**Terraform Plugin Framework** (not the legacy SDKv2). It is authored specifically to give the
showcase project first-class `cloud_init` user-data on the VM resource and a clean
create/destroy lifecycle. It is **in development** and **not published** to the Terraform Registry.

This repo is also a standalone portfolio artifact: it must read cleanly on its own to a reviewer
who never touches the main project.

## 🛑 Non-negotiable rules (read before writing code)

1. **Two-week timebox.** This provider is a project *inside* a project. The failure mode is
   building two half-finished things instead of one complete one. If S3 resources aren't done by
   the deadline, the showcase ships on the `rizalmf/idcloudhost` community provider and this
   provider finishes afterward. Do not expand scope beyond the resource list below without the
   maintainer explicitly saying so.
2. **Never claim "published" / "on the Registry."** In README, comments, docs, commit messages —
   the status is **"custom provider, in development."** Only change this if a real Registry
   publish has actually happened.
3. **Secrets never committed.** API keys / credentials live in env vars and `*.tfvars`.
   `.gitignore` must cover `*.tfvars`, `.env`, `terraform.tfstate*`, and local build artifacts.
   A leaked key is a strongly negative portfolio signal — treat it as a hard failure.
4. **Reference, don't copy.** The `rizalmf/idcloudhost` and `bapung/idcloudhost` community
   providers are *reference implementations and fallback only*. Derive the resource surface from
   the project's actual needs + IDCloudHost's actual API — do not vendor their code.

## Resource scope (the whole list)

Core (must-have, in build order):

- `idcloudhost_vm` — **the blocker for everything else; build first.**
- `idcloudhost_floating_ip`
- `idcloudhost_s3_bucket`
- `idcloudhost_s3_key` — S3 access credentials; a **separate API call** from bucket creation.
- private-network binding on the VM.

Stretch (only if the timebox has room):

- `idcloudhost_firewall`

If a resource isn't on this list, it's out of scope for v1. Say so rather than building it.

## ⚠️ API gotchas (these will bite — handle them explicitly)

- **Async VM provisioning:** create returns before the VM is ready. Implement **poll-until-ready**
  in `Create`; don't assume the resource exists the moment the call returns.
- **Errors inside 200 bodies:** IDCloudHost can return a 200 HTTP status with an error payload in
  the body. Check the body, not just the status code.
- **Bucket + key are two calls:** creating a bucket does not create credentials. `s3_key` is a
  distinct endpoint/resource.
- **`destroy` fails on non-empty buckets:** implement a **`force_destroy`** flag on `s3_bucket`
  that empties the bucket before deletion (default `false`).
- **Limited mutability:** only `name`, `vcpu`, `ram` are mutable on a VM; `vcpu`/`ram` changes
  **require a stopped VM**. Everything else is ForceNew.
- **Stale `os_version` enum:** the parameters endpoint's `os_version` list is stale vs. the live
  image-config endpoint. Prefer the live image config; don't hardcode from the stale enum.
- **Plan-time validators:** the parameters endpoint exposes valid vcpu/ram/plan values — use them
  for plan-time validation where practical.

Keep `PROVIDER_DESIGN.md` in this repo as the canonical record of these; update it when you learn
a new one.

## Commands

```bash
# Build
go build -o terraform-provider-idcloudhost

# Unit tests
go test ./...

# Acceptance tests (hit the real API — cost + real resources; run deliberately)
TF_ACC=1 go test ./... -v -run TestAcc

# Docs generation (keep docs/ in sync with schema)
tfplugindocs generate

# Local release build (no registry publish)
goreleaser build --snapshot --clean

# Manual smoke test against the real API:
#   use examples/ with a scratch .tfvars, apply, confirm, destroy.
#   You do NOT need the main repo to exist to validate this provider.
```

## Conventions

- **Plugin Framework**, typed schema models, context-aware CRUD. No SDKv2 patterns.
- Every resource: full `Create` / `Read` / `Update` / `Delete` + `ImportState` where it makes
  sense. `Read` must reconcile drift, not just no-op.
- Wrap the API in an internal client package; don't scatter `http` calls across resources.
- Return actionable diagnostics (`resp.Diagnostics.AddError`) with the API's error body included.
- Test each resource against the real API from the provider's own `examples/` dir before wiring
  it into the main project.

## Out of scope for this repo

- Docker / Compose / the application stack — that's the main showcase repo.
- cloud-init content — the provider *passes* user-data; it doesn't own the script.
- Any resource not in the scope list above.
