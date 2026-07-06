# terraform-provider-idcloudhost

> 🚧 **Custom provider, in development. NOT published to the Terraform Registry.**
> This status stays until an actual Registry publish happens — do not describe it as "published"
> before then. A resource is described as working only once its status below is ✅ **Verified**
> against the real IDCloudHost API.

A **project-scoped Terraform provider for [IDCloudHost](https://idcloudhost.com/)**, written in Go
on the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).
It exists to provision one specific showcase environment with a clean create/destroy lifecycle —
**not** to be a complete IDCloudHost provider. Scope is a feature, not a limitation.

**Why hand-write it** rather than use a community provider: to surface **first-class `cloud_init`
user-data** on the VM resource (the mechanism behind the showcase's zero-touch, no-SSH bootstrap),
and to correctly handle IDCloudHost's async lifecycle and API edges. The community
[`rizalmf/idcloudhost`](https://registry.terraform.io/providers/rizalmf/idcloudhost) provider is
used as a **reference implementation and fallback only**; the resource surface here is derived from
the project's real needs and the official IDCloudHost API.

Full design, schema, and API gotchas: **[`PROVIDER_DESIGN.md`](./PROVIDER_DESIGN.md)**.

---

## Resource status

**Legend:** 📋 planned · 🔨 in progress · ✅ verified (implemented **and** confirmed via
acceptance/smoke test against the real API).

| Resource | Purpose | Status |
| ---------- | --------- | -------- |
| `idcloudhost_vm` | VM with native `cloud_init`; async create → poll-until-ready | 🔨 create/read/delete skeleton, mock-tested only — no acceptance test run yet |
| `idcloudhost_private_network` | Private network binding for `infra-net` isolation | 📋 |
| `idcloudhost_floating_ip` | Public edge IP, assigned from the VM side | 📋 |
| `idcloudhost_s3_bucket` | Object storage for verified DR; `force_destroy` support | 📋 |
| `idcloudhost_s3_key` | S3 credentials (separate API call from bucket) | 📋 |
| `idcloudhost_firewall` | Cloud-layer 80/443 allow — **stretch, first to cut** | 📋 |

---

## Provider configuration (target)

> Interface contract — filled in and verified as resources land.

```hcl
provider "idcloudhost" {
  apikey           = var.idcloudhost_apikey  # or env IDCLOUDHOST_API_KEY
  default_location = "sgp01"                  # jkt01 | jkt02 | jkt03 | sgp01
}
```

---

## Development

```bash
# Unit tests (httptest mocks — no credentials, no cost)
go test ./...

# Build
go build -o terraform-provider-idcloudhost

# Acceptance / smoke tests — hit the REAL API and BILL REAL MONEY. Run deliberately.
#   Requires IDCLOUDHOST_API_KEY + a billing account id in the environment.
TF_ACC=1 go test ./... -v -run TestAcc

# Docs generation
tfplugindocs generate
```

**Local iteration** uses `dev_overrides` in `~/.terraformrc` (fast loop while writing Go; skips the
lock file and warns — that's fine here). The **showcase repo** consumes a *built* version via a
**filesystem mirror** instead, for reproducibility — see that repo's `setup-provider.sh`. Two
different jobs; don't conflate them.

---

## Scope & timebox

This provider is a project *inside* a project, under a **strict two-week timebox**. If the S3
resources aren't done by the deadline, the showcase ships on the `rizalmf/idcloudhost` fallback and
this provider finishes afterward — the showcase ships either way. Resources outside the table above
are out of scope for v1.

Not published to the Terraform Registry. Consume locally via the methods above.
