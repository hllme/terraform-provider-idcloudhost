# PROVIDER_DESIGN.md — `terraform-provider-idcloudhost` (project-scoped)

> Design-before-code artifact. This provider exists to serve the showcase's Pillar 0,
> not to be a complete IDCloudHost provider. Scope is a feature, not a limitation.
>
> References: `rizalmf/idcloudhost` and `bapung/terraform-provider-idcloudhost` are
> **reference implementations** for API mapping and release scaffolding only.
> The spec below is derived from the project's requirements and the official
> IDCloudHost API (<https://api.idcloudhost.com/>), not from either provider.

---

## 1. Goals & Non-Goals

### Goals

- Provision the complete showcase environment from nothing: **VM + private network
  binding + floating IP + S3 bucket + S3 credentials**.
- First-class **cloud-init user-data** support on the VM resource (the reason this
  provider exists — neither community provider surfaces it as a primary concern).
- Correct async lifecycle handling (poll-until-ready, poll-until-gone).
- Plan-time validation using the constraints the API itself publishes.
- Clean `terraform destroy` story, including the non-empty-bucket problem.

### Non-Goals (deliberate)

- Load balancers (invites the "scalable" claim the project explicitly dropped).
- Standalone block storage / disk resources.
- VM snapshots & replicas (DR is `pg_dump`-based by design — logical, verifiable).
- Billing, DNS, Kubernetes, or any other IDCloudHost product surface.
- Multi-VM topologies, import of pre-existing infra beyond basic `Importer` support.

---

## 2. Provider Configuration

```hcl
provider "idcloudhost" {
  apikey           = var.idcloudhost_apikey # or env IDCLOUDHOST_API_KEY
  default_location = "sgp01"                # jkt01 | jkt02 | jkt03 | sgp01
}
```

| Argument | Type | Required | Notes |
| --- | --- | --- | --- |
| `apikey` | string, **Sensitive** | yes (or env var) | Sent as `apikey:` header on every request |
| `default_location` | string | no | Fallback when a resource omits `location` |

**Framework:** `terraform-plugin-framework` (current standard; SDKv2 is maintenance
mode — using the modern framework is itself a differentiator vs. both references).

**HTTP client requirements:**

- Inject `apikey` header centrally; never log it.
- **Treat the response body as the source of truth for errors.** The API can return
  failures as `{"errors": {...}}` in otherwise-OK-looking responses. Every call must
  check for an `errors` key before parsing success payloads. Missing this corrupts
  Terraform state silently.
- Context-aware (respect plan/apply timeouts and cancellation).
- Retry w/ backoff on 429/5xx only. No retry on 4xx.

---

## 3. Resources

### 3.1 `idcloudhost_vm` — the core resource

API: `POST /v1/user-resource/vm` (create), `GET /v1/user-resource/vm?uuid=` (read),
modify/control/delete endpoints for update & destroy.

```hcl
resource "idcloudhost_vm" "host" {
  name               = "devops-showcase"
  billing_account_id = var.billing_account_id
  location           = "sgp01"

  os_name    = "ubuntu"
  os_version = "22.04-lts"   # validate against /v1/config/vm_images, NOT the stale parameters enum

  vcpu  = 4
  ram   = 8192               # MB
  disks = 40                 # GB

  username   = "ops"
  password   = var.vm_password          # Sensitive
  public_key = var.ssh_public_key       # break-glass only; project rule is no SSH

  cloud_init = file("${path.module}/cloud-init.yaml")  # THE feature

  private_network_uuid = idcloudhost_private_network.net.network_uuid
  float_ip_address     = idcloudhost_floating_ip.edge.address

  reserve_public_ip = false   # we attach an explicit floating IP instead
  desired_status    = "running"
  backup            = false   # platform backups off; DR pillar owns backups
}
```

#### Schema

| Attribute | Type | Behavior | Notes |
| --- | --- | --- | --- |
| `uuid` | string | **Computed**, ID | |
| `name` | string | Required, **Updatable** | Validate with API-published regex |
| `billing_account_id` | int | Required, ForceNew | |
| `location` | string | Optional, ForceNew | Defaults to provider `default_location`; `ignore_changes` pattern documented |
| `os_name` / `os_version` | string | Required, **ForceNew** | Changing OS = new machine. Live values from `/config/vm_images` |
| `vcpu` | int | Required, **Updatable (stop-required)** | API range 1–16, plan-time validated |
| `ram` | int (MB) | Required, **Updatable (stop-required)** | API range 512–65536 |
| `disks` | int (GB) | Required, **ForceNew in v1** | Disk grow is possible via disk endpoints but out of scope; document it |
| `username` | string | Required, ForceNew | |
| `password` | string | Required, ForceNew, **Sensitive** | Enforce API rule at plan time: ≥8 chars, upper+lower+digit |
| `public_key` | string | Optional, ForceNew | |
| `cloud_init` | string | Optional, **ForceNew**, semantic-YAML diff suppress | Valid YAML/JSON user-data; API merges over platform defaults. **Warning in docs: user-provided `users:` key overrides username/password injection** |
| `private_network_uuid` | string | Optional, ForceNew (v1) | |
| `float_ip_address` | string | Optional, Updatable | Assign/unassign via IP endpoints |
| `reserve_public_ip` | bool | Optional, ForceNew, default true→set false in our usage | |
| `desired_status` | string | Optional, Updatable | `running` \| `stopped` |
| `backup` | bool | Optional | Platform backup flag; default false |

#### Lifecycle logic (the real work)

- **Create:** POST → poll `GET` until `status == "running"` (or target state). States
  observed in the wild include `queued`, `building`, `installing`, `running`,
  `stopped`, `deleting` — treat unknown states as "keep polling," fail on timeout
  (default 10m, user-overridable via `timeouts` block).
- **Update (vcpu/ram):** if VM running → `stop` → poll `stopped` → `modify` →
  `start` → poll `running`. Surface this clearly in docs: **a plan that changes
  vcpu/ram implies downtime.** `name` changes are online.
- **Update (float_ip_address):** unassign old / assign new via the IP-address
  endpoints, not VM modify.
- **Delete:** DELETE → poll until `GET` 404s / errors "not found." Do not return
  success on the DELETE call alone.
- **Read:** must tolerate 404 → remove from state (drift after console deletion).
- **Import:** by UUID. `password` and `cloud_init` are not readable back —
  mark as such (standard write-only handling).

---

### 3.2 `idcloudhost_private_network`

API: private-network CRUD under `/v1/network/`.

| Attribute | Type | Behavior |
| --- | --- | --- |
| `network_uuid` | string | **Computed**, ID |
| `name` | string | Required, Updatable |
| `location` | string | Optional, ForceNew, `ignore_changes` documented |

Simple sync resource. Exists so `infra-net` isolation starts at the cloud layer,
not only inside Docker.

---

### 3.3 `idcloudhost_floating_ip`

API: `POST /v1/network/ip_addresses` (+ `/{address}/assign`, `/unassign`).

| Attribute | Type | Behavior |
| --- | --- | --- |
| `address` | string | **Computed**, ID |
| `name` | string | Required, Updatable |
| `billing_account_id` | int | Required, Updatable |
| `location` | string | Optional, ForceNew |

Assignment to a VM is driven from the **VM side** (`float_ip_address`) to avoid
two resources fighting over one relationship. Document this ownership decision.

⚠️ Floating IPs bill independently of VMs. Destroy must actually release the address.

---

### 3.4 `idcloudhost_s3_bucket`

API: `PUT /v1/storage/bucket`, `GET /v1/storage/bucket`, `DELETE /v1/storage/bucket`.

```hcl
resource "idcloudhost_s3_bucket" "dr" {
  name               = "showcase-dr-backups"
  billing_account_id = var.billing_account_id
  force_destroy      = true   # empty the bucket via S3 API before delete
}
```

| Attribute | Type | Behavior |
| --- | --- | --- |
| `name` | string | Required, ForceNew, globally-unique per platform rules |
| `billing_account_id` | int | Required, Updatable |
| `force_destroy` | bool | Optional, default `false` |
| `s3_endpoint` | string | From `GET /v1/storage/api/s3` (e.g. `s3.idcloudhost.com:8080`) — feeds cloud-init/DR scripts without hardcoding |

**The non-empty-bucket problem:** the control-plane DELETE only removes empty
buckets. With `force_destroy = true`, Delete first lists+deletes all objects via
the **S3 data plane** (AWS SDK for Go pointed at `s3_endpoint`, using keys from
3.5), then calls the control-plane DELETE. This keeps the "one command tears it
all down" claim true. With `force_destroy = false` (default), a non-empty bucket
fails destroy loudly with a clear error — safe default, opt-in convenience.
This mirrors `aws_s3_bucket.force_destroy`, so reviewers will recognize the pattern.

---

### 3.5 `idcloudhost_s3_key` (or data source — see note)

API: `GET/POST /v1/storage/user`, `POST /v1/storage/user/keys`,
`DELETE /v1/storage/user/keys`.

| Attribute | Type | Behavior |
| --- | --- | --- |
| `access_key` | string | **Computed** |
| `secret_key` | string | **Computed**, **Sensitive** |

Platform behavior: the storage user (and an initial keypair) is auto-generated
per account. Two viable models:

- **Resource** (`POST /keys` on create, `DELETE /keys` on destroy): a *dedicated*
  keypair whose lifecycle matches the environment — rotated every apply/destroy
  cycle. **Chosen.** It's the stronger security story and gives Delete real work.
- Data source (read the default key): simpler, but the key outlives the
  environment, which contradicts the ephemeral thesis.

Keys land in state (unavoidable for computed credentials — same as every cloud
provider). Mitigation is documented in README: local state, gitignored, short-lived
environment. Wire `secret_key` into cloud-init via templatefile, never into the repo.

---

### 3.6 `idcloudhost_firewall` — stretch, explicitly optional

API: full CRUD at `/v1/network/firewalls` + VM assignment.
Allow 80/443 in, deny rest; assign to the VM. Second, cloud-layer artifact for
Pillar 2's "secure networking" claim. Build only after 3.1–3.5 are green and the
showcase itself is on schedule. **If the showcase slips, this is the first cut.**

---

## 4. Validation (free wins from the API)

`GET /v1/api/parameters/vm` publishes constraints; encode them as plan-time
validators so users fail in `terraform plan`, not mid-apply:

- `name`: API-published regex
- `password`: ≥8 chars, must contain upper, lower, digit
- `vcpu`: 1–16 · `ram`: 512–65536 MB · `disks`: 20–240 GB
- `cloud_init`: must parse as YAML or JSON (client-side check before send)
- `location`: enum jkt01/jkt02/jkt03/sgp01

⚠️ Do **not** validate `os_version` against the parameters endpoint — its enum is
stale (lists 16.04). Live truth is `/v1/config/vm_images`; either fetch at plan
time via a data source or document manual verification.

---

## 5. Testing Strategy

| Layer | Tool | Cost |
| --- | --- | --- |
| Unit: client parsing, `errors`-in-body handling, validators, YAML check | Go stdlib + httptest mock server | $0 |
| Acceptance: full CRUD per resource against real API | `TF_ACC=1`, framework acctest | Real money — VM is billed per hour; run tiny (1 vCPU / 1 GB / 20 GB), destroy in test teardown, run sparingly and never in CI on every push |
| End-to-end: the showcase itself | `terraform apply` → full journey → `destroy` | The actual product |

The `errors`-in-body unit tests are the highest-value tests in the repo: they
encode the API's sharpest edge.

## 6. Release

- GoReleaser + provider-signing GPG key → publish to the Terraform Registry
  under your namespace (both reference providers show the `.goreleaser.yml` shape).
- Semver from `v0.1.0`; the showcase pins `~> 0.1`.
- Registry-standard docs (`docs/` generated via `tfplugindocs`).
- Until published, develop against a local build via `dev_overrides` in `~/.terraformrc`.

## 7. Build Order (provider only)

1. Client package: auth header, error-body handling, retry, one `GET /vm` smoke call.
2. `idcloudhost_vm` create/read/delete with polling — hardest part first.
3. `idcloudhost_private_network`, `idcloudhost_floating_ip` (sync, fast).
4. VM update paths (name online; vcpu/ram stop-modify-start; float IP swap).
5. `idcloudhost_s3_bucket` + `idcloudhost_s3_key`, incl. `force_destroy` data-plane empty.
6. Validators, importers, `tfplugindocs`, GoReleaser, registry publish.
7. (Stretch) `idcloudhost_firewall`.

**Timebox: 2 weeks of provider work.** If step 5 isn't done by then, fall back to
`rizalmf/idcloudhost` for the showcase and finish the provider afterward — the
showcase ships either way. The fallback costs one small refactor (resource names),
which is exactly the provider-portability property the architecture already claims.
