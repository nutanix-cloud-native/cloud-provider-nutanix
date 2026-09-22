---
name: sync-back-pr
description: Bring internal-only work from the private fork's `internal/main` into this public repo, replacing the internal SDK and the internal prism-go-client fork with their public equivalents and stripping internal-only wiring. Use when asked to "sync back to upstream", "upstream our internal changes", "open a sync-back PR", "pull internal changes into the public repo", or similar.
---

# sync-back-pr — upstream `internal/main` into this repo

This repo (`nutanix-cloud-native/cloud-provider-nutanix`) is the public one. A private fork,
`nutanix-cloud-native/internal-cloud-provider-nutanix`, carries internal development on its
`internal/main` branch. Two flows move code between them:

| | direction | branch | base | lives in |
|---|---|---|---|---|
| `sync-pr` | public → internal | `issue/sync-main` | `internal/main` | the internal fork |
| `sync-back-pr` | internal → public | `chore/sync-from-internal` | `main` | here |

This is the higher-risk direction: it moves code out of a private repo into a public one, and it
applies a diff produced against internal's CI, registry and dependency wiring. Everything below
exists to stop something leaking, breaking public CI, or weakening this repo's security posture.

## Before doing anything: check for an existing sync-back PR

```bash
gh pr list --repo nutanix-cloud-native/cloud-provider-nutanix --state open \
  --json number,title,headRefName,baseRefName,url
```

If a PR from `chore/sync-from-internal` is already open, update that branch in place. Never open
a duplicate.

Then check what else is in flight. A sync-back is large and will conflict with any open PR
touching `go.mod` or `pkg/provider/`. If one exists, **stack on it** (`--base <that branch>`)
rather than racing it — merging either one first would leave the other with a `go.mod` conflict.

## Before doing anything else: check prism-go-client

Unlike most sync-backs, this one has a hard cross-repo prerequisite. `internal/main` carries

```
replace github.com/nutanix-cloud-native/prism-go-client => github.com/nutanix-cloud-native/internal-prism-go-client vX.Y.Z
```

and calls converged APIs that may exist **only** in the internal fork. The public repo cannot
carry that `replace` — a public module must not resolve to a private one, and a public consumer
would fail to build. So the feature has to reach public `prism-go-client` *first*.

Check that the public module actually has what the synced code calls before writing any Go:

```bash
gh api "repos/nutanix-cloud-native/prism-go-client/contents/converged?ref=main" --jq '.[].name'
```

If a needed file is missing there, the public prism-go-client sync-back has not landed yet. Do
not work around it — no `replace`, no vendoring, no copying the source in. Either wait for that
repo's stack to merge and a tag to be cut, or, if the branch is needed now, pin a pseudo-version
at the head of its top-of-stack PR branch and say so plainly in the PR body with a TODO to re-pin:

```bash
go get github.com/nutanix-cloud-native/prism-go-client@<sha-of-top-of-stack>
```

A pseudo-version pin is a **blocked** PR, not a mergeable one. Mark it as such.

## Working safely

Use a scratch worktree so the diff can be shaped without disturbing a checkout:

```bash
git worktree add /tmp/sync-back main
cd /tmp/sync-back
git fetch <path-or-url-of-internal-fork> internal/main:internal-main
git checkout -b chore/sync-from-internal internal-main
```

Rewrite dependencies and strip internal-only files (below), then squash onto `main`.

## 1. Dependencies: public SDK only, on latest tags

Rewrite every internal SDK import to its public equivalent:

```
github.com/nutanix-core/ntnx-api-golang-sdk-internal/<mod>-go-client/v17
  ->  github.com/nutanix/ntnx-api-golang-clients/<mod>-go-client/v4
```

At the time of writing that covers `clustermgmt`, `multidomain`, `prism` and `vmm` as direct
requirements, plus `iam`, `monitoring` and `networking` as indirects. The model sub-paths are
identical between the two SDKs (`models/clustermgmt/v4/config`, `models/multidomain/v4/config`,
…), so the rewrite is purely the module prefix and major version — import aliases and every
`clusterModels.` / `vmmModels.` reference stay as they are.

**There are two modules.** `go.mod` at the root and `test/e2e/go.mod`. The e2e module picks up the
internal SDKs and the internal prism-go-client `replace` as *indirect* requirements, so it is easy
to fix the root module, see a clean build, and ship an e2e module still naming private repos.
Every dependency step below applies to both.

Drop the prism-go-client `replace` directive from both modules and require the public module
directly. Then pin each ntnx-api module to its newest tag, reading tags from the source of truth
rather than the module proxy, which can lag:

```bash
git ls-remote --tags https://github.com/nutanix/ntnx-api-golang-clients.git \
  | awk '{print $2}' | sed 's|refs/tags/||' | grep '^<mod>-go-client/' | sort -V | tail -5
```

**Gotcha that has bitten before:** if `go.mod` still holds `v17.x` versions under a `/v4` module
path, `go get` aborts with *"invalid: should be v4, not v17"* and applies **none** of the
requested pins — including ones for modules you did rewrite. It reads like a warning; it is a
total no-op, and the result is a sync silently carrying stale SDK versions. Fix the versions in
`go.mod` first, then `go get`, then verify every line:

```bash
grep ntnx-api go.mod test/e2e/go.mod   # confirm each module is on the tag you intended
```

Prefer stable tags. A prerelease is acceptable only where a needed package genuinely does not
exist in the stable tag — internal features reach public betas first. `multidomain` is the usual
offender, because the project and resource-group APIs this repo's node labelling depends on are
newer than its last stable tag. Verify rather than assume:

```bash
go mod tidy   # names the exact missing package, e.g.
              # multidomain-go-client/v4@v4.3.1 does not contain .../request/projects
```

If a beta is required, say so in the PR body and name the package that forces it. Where the
version is inherited from prism-go-client's own `go.mod`, keep the two in step — a lower pin here
than prism-go-client requires will be silently upgraded by MVS anyway, so pin it deliberately.

## 2. No internal wiring may remain

```bash
grep -rn "ntnx-api-golang-sdk-internal\|nutanix-core\|internal-prism-go-client" \
  --include="*.go" --include="*.mod" --include="*.sum" .
```

Must come back empty in **both** modules — `go.sum` included.

Internal infrastructure hostnames and private sibling repos leak just as badly as Go imports, and
they hide in YAML that no compiler checks:

```bash
grep -rn "harbor.eng.nutanix.com\|ncn-prerelease\|internal-cluster-api-provider-nutanix\|internal-cloud-provider-nutanix" \
  --exclude-dir=.git .
```

Also drop internal-only tooling that has no meaning here: the internal fork's `sync-pr` skill
(`.agents/skills/sync-pr` and its `.claude/skills` symlink) and `.claude/settings.local.json`.

**Do not delete this skill.** `sync-back-pr` lives in this repo. Depending on when the internal
fork last ran `sync-pr`, `internal/main` may not contain it, in which case the sync-back diff
will show it as a deletion. Keep it — same hazard class as the Codecov removal below.

## 3. `.github/` — the security-critical part

**Read every hunk and decide on it.** Do not reach for `git checkout main -- .github/`. A blanket
restore looks safe because it cannot leak anything, but it is a decision not to think, and it
silently reverts changes that genuinely belong here — most reliably the CI wiring for tests
arriving in the same sync, which then run unconfigured or not at all. Wholesale-keeping is
obviously wrong; wholesale-reverting is wrong in a way that passes CI.

Work through the diff hunk by hunk:

```bash
git diff main -- .github/
```

Every hunk is one of four things, and you have to say which:

1. **Internal-only infrastructure** → drop. Credentials, private registries, private module
   access, internal branch names. Catalogued below.
2. **A deletion of something only the public repo needs** → drop the deletion. The fork has no
   external contributors and no public coverage reporting, so its CI legitimately lacks
   defences this repo depends on. Catalogued below.
3. **A runner or trigger change** → drop, and treat it as a finding rather than a hunk.
   Catalogued below.
4. **Genuinely portable CI** → keep. Wiring for tests this sync adds, a real fix to a job both
   repos run. Keep it, and justify it in the PR body.

The catalogues below are the lens, not the whole answer — they list what has come up before, and
a hunk that fits none of them still needs a judgement. When a hunk mixes categories, split it:
the fork's `Test build` step carries both new project-scoped `env:` entries (keep) and a Harbor
`LOCAL_IMAGE_REGISTRY` (drop), in the same handful of lines.

### Internal additions that must NOT land here

- `GOPRIVATE` env vars — meaningless once dependencies are public.
- `actions/create-github-app-token` steps using `GHA_CHECKOUT_APP_ID` /
  `GHA_CHECKOUT_APP_PRIVATE_KEY`, and the
  `git config --global url."https://x-access-token:...@github.com/...".insteadOf` rewrites.
  These mint credentials for private orgs. This repo runs workflows for **fork PRs**;
  token-minting steps here are a credential-exposure risk.
- Harbor release and CI wiring: `HARBOR_RELEASE_USERNAME` / `HARBOR_RELEASE_PASSWORD`,
  `HARBOR_CI_USERNAME` / `HARBOR_CI_PASSWORD`, `vars.LOCAL_IMAGE_RELEASE_REGISTRY`,
  `vars.LOCAL_IMAGE_CI_REGISTRY`, and the "Build and push to Harbor (primary)" release step.
  This repo publishes to GHCR only.
- `internal/main` and `internal/release-*` branch triggers.
- Issue and PR templates rewritten to name `internal-cloud-provider-nutanix`.
- `if: github.repository == 'nutanix-cloud-native/internal-cloud-provider-nutanix'` guards. These
  never match here, so the job silently never runs. A permanently-skipped required check is worse
  than a failing one — it looks green.

### Internal removals that must NOT propagate here

A sync-back applies internal's *diff*, so a step deleted in the fork gets deleted here too. This
repo's fork-PR defences live entirely in steps the fork has no use for, which makes this the most
dangerous section on the page. Guard against each explicitly:

- **CodeQL.** `.github/workflows/codeql-analysis.yaml` is deleted outright on `internal/main` —
  a private repo where every contributor is trusted does not need it. The sync-back will present
  that as a file deletion. It must stay here.
- **`check_approvals` jobs** using `nutanix-cloud-native/action-check-approvals`, and the
  `pull_request_target` triggers plus `allow-unsafe-pr-checkout: true` they gate. The fork drops
  all of it and runs plain `pull_request`, because it has no external contributors. Removing the
  approval gate here would let an unapproved fork PR run integration jobs with secrets.
- **Codecov.** `codecov/codecov-action` with `CODECOV_TOKEN` is intentionally absent in the fork,
  because Codecov rejects tokenless uploads for private repos. It must stay here, along with the
  `EXPORT_RESULT` / `make coverage` plumbing it consumes.
- This repo's `branches: [main, release-*]` triggers.

### Runner changes — never carry these back

CodeQL here runs on `ubuntu-latest` deliberately, with a comment saying so: untrusted fork code
must compile in an ephemeral GitHub-hosted sandbox, **not** on self-hosted infrastructure. The
internal fork additionally moves jobs between self-hosted pools (`self-hosted-nutanix-small`,
`-medium`) and adds `DeterminateSystems/nix-installer-action` with
`skip-nix-installation: "true"` for its ARC runners. That is fine for a private repo where every
contributor is trusted. Carrying it here would let a fork PR execute attacker code on
Nutanix-controlled runners. Combined with `pull_request_target`, that is a textbook pwn-request.

**Rule:** jobs in this repo stay on the runner `main` puts them on, and CodeQL stays
GitHub-hosted. If a sync-back diff moves a job onto a different self-hosted runner, or introduces
`pull_request_target` where this repo used `pull_request`, stop — that is a security regression,
not a sync.

### Category 4 in practice: wiring for tests you just added

This is the category a reviewer is least likely to miss and an automated sweep is most likely to
throw away. If the sync-back brings **new tests that read CI-supplied credentials**, the `env:`
lines feeding them are part of the change, not internal-specific wiring.

Getting it wrong is worse than it sounds, because the E2E scenarios `Skip()` when their variables
are unset rather than failing. Omit the wiring and CI stays green while the new scenarios never
run once — the same "permanently-skipped check looks green" hazard as a `github.repository ==`
guard, arriving from the opposite direction.

The project-scoped scenarios need exactly three, added to the `Test build` step's `env:` in
`.github/workflows/e2e.yaml`:

```yaml
NUTANIX_PROJECT_SCOPED_PC_PROJECT: ${{ vars.NUTANIX_PROJECT_SCOPED_PC_PROJECT }}
NUTANIX_PROJECT_SCOPED_USER: ${{ secrets.NUTANIX_PROJECT_SCOPED_USER }}
NUTANIX_PROJECT_SCOPED_PASSWORD: ${{ secrets.NUTANIX_PROJECT_SCOPED_PASSWORD }}
```

`build-dev.yaml` calls `e2e.yaml` with `secrets: inherit`, so no `secrets:` declaration is needed
on the reusable workflow — a bare reference is enough. Do not carry over anything else from the
fork's version of the step; its `LOCAL_IMAGE_REGISTRY` points at Harbor.

Carry the `env:` lines in the diff, then check the names resolve. **Repo secrets and variables
live in repo settings, not in the diff**, so a sync-back cannot create them and the reviewer
cannot see they are missing:

```bash
gh variable list --repo nutanix-cloud-native/cloud-provider-nutanix
gh secret list   --repo nutanix-cloud-native/cloud-provider-nutanix
```

Diff that against the internal fork's. Anything referenced but absent must be created by someone
with admin on this repo before the tests mean anything — call it out in the PR body as a required
follow-up, with the names, since it is work the PR itself cannot do.

### Before opening the PR

Read the surviving diff once more, as a whole:

```bash
git diff main -- .github/
```

There is no expected size. What there is, is an expectation that you can name the reason for
every remaining hunk, and that each one is category 4. List them in the PR body with those
reasons. A `.github/` diff nobody can explain hunk-by-hunk is the failure this section exists to
prevent — whether it is fifty lines long or zero.

## 4. E2E assets — the second leak surface

`test/e2e/` is where this repo differs most from prism-go-client, and it is the one place where a
leak is a vendored file rather than a config line.

The internal fork vendors **private CAPX release assets** so its E2E can run against unreleased
CAPX builds:

- `test/e2e/data/capx/<version>/infrastructure-components.yaml` and `metadata.yaml`, downloaded
  from the private `nutanix-cloud-native/internal-cluster-api-provider-nutanix` releases. The
  components manifest names
  `harbor.eng.nutanix.com/ncn-prerelease/internal-cluster-api-provider-nutanix:<tag>`.
- `hack/bump-capx-e2e.sh`, which `gh release download`s those private assets, plus its
  `bump-capx-e2e` Makefile target and the `bump-capx-e2e` skill that drives it.
- `test/e2e/config/nutanix.yaml` rewired to load the provider from that vendored path and to
  preload the Harbor image.

**None of it comes back.** This repo resolves CAPX from its public GitHub release URL. Restore
the provider block and the preload image from `main`, while keeping the genuinely portable parts
of the internal change — the new cluster templates, the project-scoped variables, and the
`sourcePath` entries for those templates:

```bash
git diff main -- test/e2e/config/nutanix.yaml   # provider `value:` must stay a https:// URL
```

The cluster templates themselves (`cluster-template-*-project*.yaml`) are safe: they reference
`ghcr.io/nutanix-cloud-native/cloud-provider-nutanix/controller` and public kube-vip images.
Check rather than assume, since they are generated files that a future bump could re-point.

Any skill or doc carried over that references the vendored path (`run-e2e-tests` describes the
vendored CAPI version) needs that paragraph re-pointed at the public provider, or it documents a
directory that does not exist here.

## 5. Commit shape

Squash into a single commit on top of `main`. The internal history carries internal ticket IDs
and internal-CI commits describing infrastructure that does not exist here, and a merge would
import commits whose content this skill then reverts — incoherent public history.

Squashing costs per-commit authorship, so preserve it explicitly:

```bash
git log --format='%an <%ae>' main..internal-main | sort -u \
  | grep -viE "noreply@github|dependabot|actions@github"
```

Add each as a `Co-authored-by:` trailer. This is a public repo; do not erase contributors.

Per global instructions, no Claude/Anthropic attribution in commit messages or PR descriptions.

## 6. Verify before opening the PR

```bash
go build ./... && go vet ./...
go test ./...
go vet -C test/e2e -tags=e2e ./...
go test -C test/e2e -tags=e2e -run '^$' .   # compile/smoke check, provisions nothing
gofmt -l . | grep -v '^test/e2e/data/'
```

The e2e module is excluded from the root module's `./...`, so a root-only build proves nothing
about it. Run both.

`gofmt` matters more here than it looks: the internal `constants.go` const block has been left
misaligned by internal edits more than once, and `gofmt` is the only thing that catches it.
Compare against `main` and only fix new offenders.

The CAPX E2E suite provisions real VMs against a live Prism Central and needs
`NUTANIX_ENDPOINT`, `NUTANIX_USER`/`NUTANIX_PASSWORD`, `NUTANIX_PRISM_ELEMENT_CLUSTER_NAME`,
`NUTANIX_SUBNET_NAME`, `CONTROL_PLANE_ENDPOINT_IP` and a reachable image registry — `ko.local`
only works when the cluster under test is the local kind bootstrap cluster. The project-scoped
scenarios need a PC on **7.6 or newer**; below that the code takes the no-projects path and the
assertions are meaningless. If you cannot reach such a cluster, say so plainly in the PR body
rather than implying the sync was verified end to end.

## 7. PR body

Title: `feat: sync internal fork changes using public API SDKs`

Include:

1. **What is being upstreamed** — the new public API surface, in a paragraph.
2. **SDK versions**, the public prism-go-client version, and any prerelease or pseudo-version pin
   with the reason and the package that forces it.
3. **What was deliberately excluded** — internal CI, Harbor wiring, vendored CAPX assets, the
   `sync-pr` and `bump-capx-e2e` skills — and why.
4. **Anything a reviewer would otherwise miss** — exported-constant renames, call-convention
   changes, behavioural changes, inverted test assertions.
5. **Honest test status**, including what was not run.

## Checklist

- [ ] No duplicate sync-back PR; stacked on any conflicting in-flight PR
- [ ] Public prism-go-client actually carries every API the synced code calls
- [ ] No `replace` directive to `internal-prism-go-client` in either module
- [ ] Zero `nutanix-core` / `ntnx-api-golang-sdk-internal` references, `go.sum` included
- [ ] Zero `harbor.eng.nutanix.com`, `ncn-prerelease` or private-sibling-repo references anywhere
- [ ] Both `go.mod` **and** `test/e2e/go.mod` cleaned and pinned
- [ ] Every public SDK module on its newest appropriate tag, re-checked in `go.mod` after `go get`
- [ ] Prereleases justified by a genuinely missing package, and named in the PR body
- [ ] Every surviving `.github/` hunk reviewed individually and its reason stated in the PR body
- [ ] New tests' `env:` wiring carried over, and every secret/var it names exists in this repo's
      settings — or the missing names are flagged in the PR body as a required follow-up
- [ ] CodeQL, `check_approvals` + `pull_request_target`, and Codecov still present
- [ ] No job moved to a different self-hosted runner; no new `pull_request_target`
- [ ] No vendored CAPX assets; e2e config resolves the provider from its public release URL
- [ ] `sync-pr`, `bump-capx-e2e` and `.claude/settings.local.json` not carried over; this skill
      not deleted
- [ ] Single squashed commit with `Co-authored-by:` trailers for every human author
- [ ] Build, vet and unit tests pass in both modules; unrun E2E disclosed in the PR body
