# env-sync

English | [日本語](README.ja.md)

A Go CLI that bulk-registers (syncs) environment variables declared in an `env-sync.yaml` definition file to **Vercel**, **GitHub Actions**, **GCP Secret Manager** and/or **Cloudflare Workers**.
From a single definition file you can choose the sync target per variable and push to several providers at once.

- **Vercel**: Uses the REST API (`POST /v10/projects/{id}/env?upsert=true`), so re-running **upserts** existing variables.
- **GitHub Actions**: Supports both Secrets (sealed-box encrypted) and Variables (plaintext).
- **GCP Secret Manager**: Adds a new Secret version per run. Only `secret: true` entries are synced.
- **Cloudflare Workers**: Registers Worker Secrets via the REST API. Only `secret: true` entries are synced (see [Sync to Cloudflare Workers](#5-sync-to-cloudflare-workers)).

## How It Works

- **`secret` / `environments` are declared explicitly in `env-sync.yaml`** — no heuristics based on key names.
- **Values are never written to `env-sync.yaml`** (it is committed to git). Values are read from `.env(.production)`.
- Keys not declared in the definition are not registered (warned and skipped even if present in `.env`).
- Keys declared but missing from `.env` are also warned and skipped.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install ptyhard/tap/env-sync
```

### go install

```bash
go install github.com/ptyhard/env-sync/cmd/env-sync@latest
```

### Download Binaries

Download the latest binary from [GitHub Releases](https://github.com/ptyhard/env-sync/releases).
Archives are provided for darwin/amd64, darwin/arm64, linux/amd64, and linux/arm64.

### Build from Source

```bash
go build -o env-sync ./cmd/env-sync
```

## Quick Start

```bash
# 1. Interactively generate auth config (Vercel token / project_id / GitHub token / repo)
env-sync setup

# 2. Generate a definition file scaffold from an existing .env
env-sync init

# 3. Review the generated env-sync.yaml — check secret / environments / provider

# 4. Preview what would be registered without sending (dry-run)
VERCEL_TOKEN=xxxxx env-sync --env .env.production --dry-run

# 5. Deploy for real (prompts y/N only when updates exist)
VERCEL_TOKEN=xxxxx env-sync --env .env.production
```

## 1. Generate Auth Config with `setup`

Interactively generates an auth config file (`.env-sync.config.yaml` or `~/.config/env-sync/config.yaml`).

```bash
# Generate project config (.env-sync.config.yaml)
env-sync setup

# Generate global config (~/.config/env-sync/config.yaml)
env-sync setup --global

# Overwrite existing file (without --force, overwrite is refused with an error)
env-sync setup --force
```

- For each of Vercel / GitHub, prompts whether to configure, project_id, repo, and token input method
- **Tokens default to the environment variable reference format `${VERCEL_TOKEN}` / `${GITHUB_TOKEN}`** (recommended — avoids writing plaintext tokens to the file)
- When writing raw tokens directly or with `--global`, the file is created with **0600** permissions
- In non-interactive environments (no TTY), exits with an error and instructions for manual configuration

> **Note**: It is recommended to add `.env-sync.config.yaml` to `.gitignore` to prevent committing this file.

## 2. Generate Variable Definition Scaffold with `init`

Reads an existing `.env` and auto-generates an `env-sync.yaml` scaffold without values.

```bash
# Basic (generate env-sync.yaml from .env)
env-sync init

# Specify a different file
env-sync init --env .env.production

# Specify output destination
env-sync init --env .env.production --def env-sync.production.yaml

# Overwrite existing file (without --force, overwrite is refused with an error)
env-sync init --env .env.production --force
```

- Keys with the `NEXT_PUBLIC_` prefix default to `secret: false`; all others default to `secret: true`
- This is only a **scaffold** — always review secrets before deploying
- **No values are written** (`.env` values never leak into the yaml)
- If `env-sync.yaml` already exists, overwrite is refused without `--force`

## 3. Sync to Vercel

### Prerequisites

```bash
# Link the project (first time only — creates .vercel/project.json)
vercel link

# Issue an access token
#   https://vercel.com/account/tokens
```

### Sync

```bash
# dry-run (shows planned registrations with new/update classification — values are not shown)
VERCEL_TOKEN=xxxxx env-sync --env .env.production --dry-run

# Deploy (classifies as new/update and displays — prompts y/N only when updates exist)
VERCEL_TOKEN=xxxxx env-sync --env .env.production

# Skip update confirmation (for CI, etc.)
VERCEL_TOKEN=xxxxx env-sync --env .env.production --yes
```

Before sending, the tool queries the provider and classifies each key as "`+ KEY [new]`" or "`⟳ KEY [update]`".
**The `y/N` confirmation prompt only appears when there are updates (overwrites).** New-only registrations proceed without confirmation.
Use `--yes` (`-y`) to skip the confirmation. In non-interactive environments (no TTY), if updates exist without `--yes`, the tool exits with an error for safety.
During `--dry-run`, new/update classification is shown if the token is set (nothing is sent).

## 4. Sync to GitHub Actions

Use `--provider github` to sync to GitHub Actions Secrets/Variables.

```bash
# dry-run (shows planned registrations with new/update classification — values are not shown)
GITHUB_REPO=owner/repo env-sync --provider github --env .env.production --dry-run

# Deploy (repository level — prompts only when updates exist)
GITHUB_TOKEN=xxxxx GITHUB_REPO=owner/repo env-sync --provider github --env .env.production

# Register to a named environment (specified via the environments field in env-sync.yaml)
# Write environments: [production] in the yaml to register to that environment
GITHUB_TOKEN=xxxxx env-sync --provider github --env .env.production

# Skip update confirmation (for CI, etc.)
GITHUB_TOKEN=xxxxx env-sync --provider github --yes --env .env.production
```

If `GITHUB_REPO` is omitted, `owner/repo` is auto-detected from `git remote origin`.

### GitHub Configuration Example in `env-sync.yaml`

```yaml
defaults:
  secret: true

variables:
  DATABASE_URL:
    secret: true                      # Repository-level Secret
  PUBLIC_FLAG:
    secret: false                     # Repository-level Variable
  API_KEY_PROD:
    secret: true
    environments: [production]        # Production environment Secret
  FEATURE_FLAG:
    secret: false
    environments: [production, staging]  # Production & staging Variable
```

> **Note**: Named environments specified in `environments` must be pre-created on GitHub. Specifying a non-existent environment name results in an error.

### Security

- Variables with `secret: true` are **encrypted with GitHub's public key using sealed-box encryption** before PUT. Plaintext is never sent.
- **Values are never shown** in the registration list, confirmation prompt, or success/failure logs. Same for `--dry-run`.
- Public key length (32 bytes) is validated; invalid keys abort the operation.
- Public key cache is managed per envScope (environment), as keys differ by scope.

## 5. Sync to Cloudflare Workers

Use `--provider cloudflare` to sync **Worker Secrets** via the Cloudflare REST API
(`PUT /accounts/{account_id}/workers/scripts/{script_name}/secrets`).

### Prerequisites

- An API token with the **Workers Scripts:Edit** permission (https://dash.cloudflare.com/profile/api-tokens)
- Your account ID (Workers & Pages → Overview, or `wrangler whoami`)
- The Worker script must already exist — env-sync registers secrets on a deployed Worker, it does not create one

### Sync

```bash
# dry-run (values are not shown)
CLOUDFLARE_API_TOKEN=xxxxx CLOUDFLARE_ACCOUNT_ID=yyyyy \
  env-sync --provider cloudflare --env .env.production --dry-run

# Deploy (prompts y/N only when updates exist)
CLOUDFLARE_API_TOKEN=xxxxx CLOUDFLARE_ACCOUNT_ID=yyyyy \
  env-sync --provider cloudflare --env .env.production

# Skip update confirmation (for CI, etc.)
CLOUDFLARE_API_TOKEN=xxxxx CLOUDFLARE_ACCOUNT_ID=yyyyy \
  env-sync --provider cloudflare --env .env.production --yes
```

### Secrets only — plain vars are skipped

Cloudflare Workers has two kinds of environment variables, and env-sync only syncs **Secrets**:

| Declaration | Behavior |
|-------------|----------|
| `secret: true` | Registered as a Worker Secret. **Preserved across `wrangler deploy`** |
| `secret: false` | **Skipped with a warning** |

Plain text vars are owned by the `[vars]` section of your wrangler config. Values set through the API or
dashboard are overwritten by the next `wrangler deploy`, so syncing them would silently lose data.
Keep plain vars in `wrangler.jsonc` / `wrangler.toml` and let env-sync handle only the secrets.

### `environments` map to separate Worker scripts

wrangler deploys `[env.staging]` as a **separate Worker** named `<script>-staging`. env-sync follows the
same convention, so each declared environment resolves to its own Worker script:

```yaml
# env-sync.yaml (script name resolved as "my-worker")
variables:
  API_KEY:      { secret: true, provider: cloudflare }
  DATABASE_URL: { secret: true, provider: cloudflare, environments: [production, staging] }
```

| Declaration | Target Worker script |
|-------------|----------------------|
| `environments` omitted | `my-worker` |
| `environments: [staging]` | `my-worker-staging` |
| `environments: [production, staging]` | `my-worker-production` and `my-worker-staging` |

If your production Worker is the base script rather than `my-worker-production`, override the mapping in the
auth config file:

```yaml
# .env-sync.config.yaml
cloudflare:
  api_token: ${CLOUDFLARE_API_TOKEN}
  account_id: <account ID>
  environments:
    production: my-worker          # production writes to the base script
    staging: my-worker-staging
```

Environment names not listed in `cloudflare.environments` still fall back to the `<script>-<environment>` convention.

### Worker script name resolution

Resolution priority (highest first):

1. `CLOUDFLARE_SCRIPT_NAME` environment variable
2. `cloudflare.script` in the auth config file
3. The `name` field of `wrangler.jsonc` → `wrangler.json` → `wrangler.toml` in the current directory

If none resolves, the run stops with an error.

### Multiple Workers (monorepo)

Define `cloudflare.scripts[]` to sync to several Workers in one run, and narrow it with `--cloudflare-script`:

```yaml
# .env-sync.config.yaml
cloudflare:
  api_token: ${CLOUDFLARE_API_TOKEN}   # Fallback when api_token is omitted per script
  account_id: <account ID>             # Fallback when account_id is omitted per script
  scripts:
    - name: api                        # Identifier for filtering with --cloudflare-script
      script: my-api-worker
    - name: cron
      script: my-cron-worker
      api_token: ${CRON_CF_TOKEN}      # Different token for this Worker only
```

```bash
# Sync to all defined Workers
env-sync --provider cloudflare --env .env.production

# Filter to a specific Worker
env-sync --provider cloudflare --env .env.production --cloudflare-script cron
```

### Diagnose the configuration

```bash
# Read-only check of token / accountId / script name and API reachability
env-sync validate --provider cloudflare
```

## 6. Mixing Providers

By specifying `provider` per variable, you can sync to both Vercel and GitHub Actions simultaneously from a single `env-sync.yaml`.

```yaml
defaults:
  secret: true
  # Set defaults.provider to change the default sync target for all variables
  # provider: github

variables:
  DATABASE_URL:
    secret: true
    provider: vercel            # Sync to Vercel only

  GITHUB_ACTIONS_TOKEN:
    secret: true
    provider: github            # Sync to GitHub Actions only

  SHARED_SECRET:
    secret: true
    provider: [vercel, github]  # Sync to both

  PUBLIC_API_URL:
    secret: false               # No provider specified → defaults to --provider flag (vercel)
```

```bash
# Provide both tokens to sync to Vercel and GitHub in a single run
VERCEL_TOKEN=xxxxx GITHUB_TOKEN=yyyyy env-sync --env .env.production
```

Resolution priority (highest first): **per-variable `provider`** → **`defaults.provider`** → **CLI `--provider` flag** (default `vercel`)

Invalid values (anything other than `vercel` / `github`) cause an error. During `--dry-run`, the `providers` column shows the routing for each variable.

## 7. Managing Auth Credentials and IDs via Config File

Instead of environment variables, you can manage tokens and IDs in a YAML file, eliminating the need to pass `VERCEL_TOKEN=...` every time.

### Config File Locations

| Type | Path |
|------|------|
| global | `~/.config/env-sync/config.yaml` (respects `XDG_CONFIG_HOME`) |
| project | `.env-sync.config.yaml` (current directory) |

If neither file exists, the tool falls back to environment variables and existing fallbacks only (backward compatible).

### Resolution Priority

```
Environment variables > project config > global config > existing fallbacks (.vercel/project.json / git remote)
```

Environment variables always take precedence when set.

### Schema

```yaml
vercel:
  token:      <Vercel access token>
  project_id: <project ID>
  team_id:    <team (org) ID>
github:
  token: <GitHub access token>
  repo:  <owner/repo format repository name>
cloudflare:
  api_token:  <Cloudflare API token>
  account_id: <account ID>
  script:     <Worker script name (optional — falls back to the wrangler config)>
  environments:                    # optional: environment name -> Worker script name
    staging: my-worker-staging
```

### Multiple Vercel Projects / Multiple GitHub Repos (Monorepo)

For monorepos (e.g., Turborepo) with multiple apps in a single repository, define multiple sync targets using `vercel.projects` / `github.repos` arrays. A single run distributes environment variables to all defined targets.

```yaml
vercel:
  token: <shared token>              # Fallback when token is omitted per project
  team_id: team_xxxxxxxx             # Fallback when team_id is omitted per project
  projects:
    - name: web                      # Identifier for filtering with --vercel-project
      project_id: prj_web
    - name: admin
      project_id: prj_admin
      team_id: team_yyyyyyyy         # Different team for this project only
      token: ${ADMIN_VERCEL_TOKEN}   # Different token for this project only

github:
  token: <shared token>              # Fallback when token is omitted per repo
  repos:
    - name: web
      repo: myorg/web
    - name: infra
      repo: myorg/infra
      token: ${INFRA_GH_TOKEN}      # Different token for this repo only
```

- When `token` / `team_id` is omitted for a target, it falls back to the top-level `vercel.token` / `vercel.team_id` / `github.token` (and then environment variables).
- `token` values support `${VAR}` / `${VAR:-default}` references.
- **When multiple targets are defined, `project_id` / `repo` is required for each** (`.vercel/project.json` / git remote fallback is only available for single-target configurations).

```bash
# Sync to all defined Vercel projects
env-sync --env .env.production

# Filter to a specific project / repo
env-sync --env .env.production --vercel-project admin
env-sync --provider github --env .env.production --github-repo infra
```

At runtime, each target displays a heading with the target project/repo and a new/update classification list. If one target fails, the remaining targets continue, and the exit code is 1 if any target failed.

> **Backward compatible**: Without `projects` / `repos`, the tool syncs to a single target using `vercel.project_id` / `github.repo` (plus environment variables / `.vercel/project.json` / git remote fallback) as before.

#### Per-Variable Vercel Project Targeting (`vercel_project`)

The `--vercel-project` flag narrows the entire run to one project, but you can also specify `vercel_project` on each variable in `env-sync.yaml` to target specific Vercel projects **per variable** by `name` (requires `vercel.projects[]` to be defined).

```yaml
# env-sync.yaml
defaults:
  secret: true
  # vercel_project: web        # Setting in defaults makes it the default target for all variables

variables:
  WEB_API_URL:   { provider: vercel, vercel_project: web }          # Send to web only
  ADMIN_API_URL: { provider: vercel, vercel_project: admin }        # Send to admin only
  SHARED_DB_URL: { provider: vercel, vercel_project: [web, admin] } # Send to both web and admin
  COMMON_KEY:    { provider: vercel }                               # No vercel_project → all projects (backward compatible)
```

- **Resolution priority**: Per-variable `vercel_project` > `defaults.vercel_project`. Variables without it are sent to all resolved Vercel targets (backward compatible).
- **Combined with CLI `--vercel-project` (AND logic)**: The CLI narrows the target set first, then per-variable `vercel_project` further filters within that set.
- The `name` in `vercel_project` refers to `vercel.projects[].name`. Specifying a non-existent `name` causes an error.
- `vercel_project` cannot be used in single-resolution mode (without `vercel.projects[]` defined) — it will cause an error.

### Usage Example

```bash
# Create global config (~/.config/env-sync/config.yaml)
mkdir -p ~/.config/env-sync
cat > ~/.config/env-sync/config.yaml <<'EOF'
vercel:
  token: your-vercel-token
github:
  token: your-github-token
EOF
chmod 0600 ~/.config/env-sync/config.yaml

# Override with project-specific IDs in project config
cat > .env-sync.config.yaml <<'EOF'
vercel:
  project_id: prj_xxxxxxxx
  team_id: team_xxxxxxxx
github:
  repo: myorg/myrepo
EOF

# Run without environment variables
env-sync --env .env.production
```

### Using `--environments` to Scope Writes to Specific Environments

With a single `env-sync.yaml` definition file, you can target different environments at sync time using `--environments`.

```yaml
# env-sync.yaml
variables:
  DATABASE_URL:
    secret: true
    environments: [production, staging, preview]
  API_KEY:
    secret: true
    environments: [production]
  DEBUG_MODE:
    secret: false
    environments: [staging, preview]
```

```bash
# Sync only to production (variables with no 'production' in environments are skipped)
env-sync --env .env.production --def env-sync.yaml --environments production

# Sync only to staging and preview
env-sync --env .env.staging --def env-sync.yaml --environments staging,preview

# Prune + limit deletion scope to staging only
env-sync --env .env.staging --def env-sync.yaml --environments staging --prune
```

- Variables whose declared `environments` have **no overlap** with `--environments` are skipped (warning to stderr).
- Variables that have **no `environments` declared** in the definition file are also skipped when `--environments` is specified (they have no declared environments to intersect with).
- If **all variables are skipped**, no changes are sent to any provider and prune is also skipped — the tool exits successfully with code 0.
- `--environments` must contain names that appear in `environments` fields of the definition file, or the standard values `production` / `preview` / `development`. An invalid name causes an error and exits with code 1.
- Environment names are **case-sensitive** — they must match the declaration in `env-sync.yaml` exactly.
- **GCP**: GCP Secrets have no per-environment scope. Variables that pass the filter will still have the `managed-by=env-sync` label applied/updated as a side effect of syncing. When `--prune` is combined with `--environments`, GCP prune scope is not narrowed (all `managed-by=env-sync` secrets are considered for pruning).

### Environment Variable References `${VAR}` / `${VAR:-default}`

Config values can include environment variable references (useful for avoiding plaintext tokens in config files).

| Syntax | Behavior |
|--------|----------|
| `${VAR}` | Expands to the value of environment variable `VAR`. Errors if `VAR` is unset (including empty) |
| `${VAR:-default}` | Uses `VAR`'s value if set; otherwise uses `default` |

```yaml
vercel:
  token: ${MY_VERCEL_TOKEN}          # Errors if MY_VERCEL_TOKEN is unset
  project_id: ${V_PID:-prj_default}  # Falls back to prj_default if V_PID is unset
github:
  token: ${GH_TOKEN}
  repo: ${GH_REPO:-myorg/myrepo}
```

> **Note**: The existing priority order "environment variables > project config > global config" is maintained.
> Even with `token: ${VERCEL_TOKEN}` in config, if the `VERCEL_TOKEN` environment variable is set, the env var takes direct precedence — effectively the same value.
> This feature is most useful with **aliased variables** (e.g., `${MY_VERCEL_TOKEN}`).

> **Security**: If the global config contains tokens and the file permissions are not `0600`, a warning is printed to stderr at runtime. Fix with `chmod 0600 ~/.config/env-sync/config.yaml`. Encrypted storage and keychain integration are out of scope (future consideration).

## Definition File `env-sync.yaml`

```yaml
defaults:
  secret: true

variables:
  NEXT_PUBLIC_FIREBASE_API_KEY: { secret: false }
  DATABASE_URL: { secret: true, environments: [production] }
  API_SECRET: { secret: true }
```

### Schema

| Field | Type | Description |
|-------|------|-------------|
| `prune` | `bool` (top-level) | `true`: delete remote variables not declared in `variables` during sync (default `false`). Can also be enabled with the `--prune` flag. See [Pruning Undefined Variables](#pruning-undefined-variables-prune) |
| `prune_exclude` | `[]string` (top-level) | Glob patterns of key names to never delete during prune (e.g. `[BLOB_*, SENTRY_DSN]`). Case-insensitive |
| `secret` | `bool` | `true` (default): register as secret / `false`: register as plaintext |
| `environments` | `[]string` | Array of target environments. Inherits from `defaults.environments` if omitted |
| `provider` | `string` or `[]string` | Sync target provider. Falls back to `defaults.provider` → CLI `--provider` flag if omitted |
| `vercel_project` | `string` or `[]string` | Target Vercel project name(s) (`vercel.projects[].name`). Falls back to `defaults.vercel_project` → all Vercel targets. See [Per-Variable Vercel Project Targeting](#per-variable-vercel-project-targeting-vercel_project) for details |

#### Field Meanings for Vercel

| `secret` | Vercel type | Description |
|-----------|-------------|-------------|
| `true` | `sensitive` | For secrets — values cannot be read after saving |
| `false` | `plain` | Plaintext without encryption |

`environments` accepts `production` / `preview` / `development`. Defaults to `production` and `preview` when omitted.

`environments` also accepts custom environment slugs (e.g. `staging`). Custom environments must be pre-created in the Vercel project; a non-existent slug results in an error. Standard and custom values can be mixed (e.g. `[production, staging]`).

#### Field Meanings for GitHub Actions

| `secret` | Target | Description |
|-----------|--------|-------------|
| `true` | GitHub Actions Secrets | Sealed-box encrypted. Values cannot be viewed after registration |
| `false` | GitHub Actions Variables | Plaintext. Values are visible in the GitHub UI |

Specifying a named environment in `environments` registers to that environment scope. When omitted, registers at the repository level.

> **Note**: GitHub named environments must be pre-created. Specifying a non-existent environment name results in an error.

#### Field Meanings for Cloudflare Workers

| `secret` | Target | Description |
|-----------|--------|-------------|
| `true` | Worker Secret | Registered via the secrets API. Values cannot be read after registration |
| `false` | — | **Skipped with a warning** (plain vars are owned by the wrangler config `[vars]` section) |

`environments` resolves to a Worker script name per environment (`<script>-<environment>` by default).
See [Sync to Cloudflare Workers](#5-sync-to-cloudflare-workers) for the mapping rules and how to override them.

### Pruning Undefined Variables (`prune`)

When `prune: true` is set at the top level of `env-sync.yaml` (or the `--prune` flag is passed), variables that exist on the remote but are **not declared in `variables`** are deleted during sync.

```yaml
prune: true
prune_exclude:
  - BLOB_*      # e.g. keep tokens provisioned by Vercel Blob Store
  - SENTRY_DSN

variables:
  DATABASE_URL: { secret: true }
```

Safety behavior:

- Deletion targets are listed before sending, and a `y/N` confirmation is always required when any deletion is planned (`--yes` skips it; `--dry-run` only shows the plan).
- Keys declared in `variables` are always kept — even if they have no value in the `.env` file.
- Keys matching `prune_exclude` glob patterns are kept (case-insensitive).
- **Vercel**: system variables and integration-provisioned variables (those with a `configurationId`, e.g. created by Blob Store or Marketplace integrations) are automatically excluded.
- **GitHub**: Actions Secrets and Variables are pruned at the repository level and in named environments that appear in the definition file.
- **GCP**: only Secrets labeled `managed-by=env-sync` are pruned. env-sync adds this label automatically when syncing, so Secrets created by anything else are never touched.
- **Cloudflare**: Worker Secrets are pruned only on the scripts that the definition file targets. Secrets on Workers that never appear in the run are untouched.

## Options / Environment Variables

| Item | Required | Description |
|------|----------|-------------|
| `--provider <name>` | – | Sync target (default `vercel`). Currently `vercel` / `github` / `gcp` / `cloudflare` are available |
| `--vercel-project <name>` | – | Filter sync to a single `vercel.projects[].name` from config. All defined projects if unspecified |
| `--github-repo <name>` | – | Filter sync to a single `github.repos[].name` from config. All defined repos if unspecified |
| `--cloudflare-script <name>` | – | Filter sync to a single `cloudflare.scripts[].name` from config. All defined Workers if unspecified |
| `--env <file>` | – | Env file to read values from (default `.env`) |
| `--def <file>` | – | Definition YAML (default `env-sync.yaml`) |
| `--dry-run` | – | Show planned registrations with new/update classification without sending |
| `--yes` / `-y` | – | Skip confirmation when updates (overwrites) exist |
| `--prune` | – | Delete remote variables not in the definition file (also enabled by `prune: true` in the definition file) |
| `--environments <list>` | – | Comma-separated list of environments to write to (e.g. `staging,preview`). Intersects with each variable's declared `environments`; variables with no overlap are skipped. When combined with `--prune`, deletion scope is also limited to the specified environments |
| `--force` | – | Overwrite existing def file during `init` |
| `VERCEL_TOKEN` | Yes (Vercel) | Vercel access token (not required for dry-run) |
| `VERCEL_PROJECT_ID` | Conditional (Vercel) | Project ID. Auto-detected from config file or `.vercel/project.json` if unset |
| `VERCEL_TEAM_ID` | – (Vercel) | Team (org) ID. Falls back to config file or `.vercel/project.json` `orgId` |
| `GITHUB_TOKEN` | Yes (GitHub) | GitHub access token (not required for dry-run) |
| `GITHUB_REPO` | – (GitHub) | `owner/repo` format. Auto-detected from config file or `git remote origin` if unset |
| `CLOUDFLARE_API_TOKEN` | Yes (Cloudflare) | API token with the Workers Scripts:Edit permission (not required for dry-run) |
| `CLOUDFLARE_ACCOUNT_ID` | Yes (Cloudflare) | Target account ID. Falls back to `cloudflare.account_id` in the config file. Not required for dry-run, but without it the new/update classification is skipped |
| `CLOUDFLARE_SCRIPT_NAME` | – (Cloudflare) | Worker script name. Falls back to the config file or the `name` field of the wrangler config |
| `--lang <code>` | – | Display language (`en` / `ja`). Default: `en` |
| `ENV_SYNC_LANG` | – | Display language code (`en` / `ja`). Lower priority than `--lang` |

## Display Language

CLI messages can be displayed in English (default) or Japanese.

### Priority (highest to lowest)

1. `--lang <code>` flag
2. `ENV_SYNC_LANG` environment variable
3. `language:` field in config file (`.env-sync.config.yaml` / `~/.config/env-sync/config.yaml`)
4. Default: `en`

### Config File Example

```yaml
# .env-sync.config.yaml
language: ja
vercel:
  token: ${VERCEL_TOKEN}
  project_id: <project ID>
```

### Supported Languages

| Code | Language |
|------|----------|
| `en` | English (default) |
| `ja` | Japanese |

Invalid or unsupported codes fall back to `en`.
