package i18n

// enCatalog は英語メッセージカタログ。
var enCatalog = map[MsgKey]string{
	// ----- CLI フラグ共通 -----
	MsgFlagNeedsValue:          "Error: %s requires a value\n",
	MsgFlagNeedsNonEmpty:       "Error: %s requires a non-empty value\n",
	MsgFlagUnknown:             "Error: unknown argument: %s\n",
	MsgFlagProviderInvalid:     "Error: --provider must be one of: %s\n",
	MsgFlagOrSeparator:         " or ",
	MsgInvalidEnvironmentsFlag: "--environments: invalid environment name(s): %s (allowed: %s)",

	// ----- Main / run() -----
	MsgErrorPrefix:               "Error: %s\n",
	MsgEnvFileNotFound:           "env file not found: %s",
	MsgDefFileNotFound:           "definition file not found: %s",
	MsgEnvFileReadFail:           "failed to read env file: %s",
	MsgDefFileReadFail:           "failed to read definition file: %s",
	MsgDefFileYAMLFail:           "failed to parse definition file YAML: %s",
	MsgSkipNoValueInEnv:          "⚠ %s: defined but no value in %s, skipping\n",
	MsgSkipNotDefined:            "⚠ %s: in %s but not defined, skipping\n",
	MsgSkipNoMatchingEnvironment: "⚠ %s: no matching environments after --environments filter, skipping\n",
	MsgUsage: `env-sync - sync environment variables declared in a definition file to Vercel, GitHub Actions, GCP or Cloudflare Workers

Subcommands:
  init      generate env-sync.yaml template from .env
  setup     interactively generate an auth config file (.env-sync.config.yaml / ~/.config/env-sync/config.yaml)
  validate  verify token / projectId / repo and check API reachability (read-only, no writes)

Usage:
  VERCEL_TOKEN=xxxxx env-sync [options]
  GITHUB_TOKEN=xxxxx env-sync --provider github [options]
  CLOUDFLARE_API_TOKEN=xxxxx env-sync --provider cloudflare [options]
  env-sync init [--env <file>] [--def <file>] [--force]
  env-sync setup [--global] [--force]

Options (sync):
  --provider <name>         sync destination (default: vercel)
  --env <file>              env file to read values from (default: .env)
  --def <file>              definition YAML (default: env-sync.yaml)
  --dry-run                 show planned entries (new/update) without sending (values not shown)
  --yes, -y                 skip confirmation when updates are included
  --prune                   delete remote variables not in the definition file (also enabled by prune: true in the definition file)
  --environments <list>     comma-separated list of environments to write to (e.g. staging,preview)
                            intersects with each variable's declared environments; variables with no matching environment are skipped
                            when combined with --prune: deletion scope is also limited to the specified environments
  --vercel-project <name>   sync only the named project from config vercel.projects (monorepo)
  --github-repo <name>      sync only the named repo from config github.repos (monorepo)
  --cloudflare-script <name>
                            sync only the named target from config cloudflare.scripts (monorepo)
  --lang <code>             display language (en / ja, default: en)
  --version                 show version and exit
  -h, --help                show this help

Confirmation behavior:
  Before sending, queries the provider and shows existing keys as "⟳ KEY [update]" and new keys as "+ KEY [new]".
  Confirmation prompt appears only when updates exist; new-only proceeds without prompt.
  In non-interactive environments (no TTY) with updates and no --yes/-y, stops with an error.

Options (init):
  --env <file>   env file to read (default: .env)
  --def <file>   YAML file to output (default: env-sync.yaml)
  --force        overwrite existing def file

Options (setup):
  --global       output to ~/.config/env-sync/config.yaml (respects XDG_CONFIG_HOME, default: .env-sync.config.yaml)
  --force        overwrite existing config file

Environment variables (Vercel):
  VERCEL_TOKEN       Vercel access token (required, not needed for dry-run)
  VERCEL_PROJECT_ID  project ID; if not set, read from config file or .vercel/project.json
  VERCEL_TEAM_ID     team (org) ID; if not set, read from config file or .vercel/project.json orgId

Environment variables (GitHub):
  GITHUB_TOKEN  GitHub access token (required, not needed for dry-run)
  GITHUB_REPO   repository in owner/repo format (if not set, read from config file or git remote origin)

Environment variables (GCP):
  GCP_PROJECT_ID  target GCP project ID for Secret Manager (required)
  Auth: uses Application Default Credentials (ADC).
        Set GOOGLE_APPLICATION_CREDENTIALS for a service account key,
        or run gcloud auth application-default login to configure ADC.

Environment variables (Cloudflare Workers):
  CLOUDFLARE_API_TOKEN   Cloudflare API token with the Workers Scripts:Edit permission (required, not needed for dry-run)
  CLOUDFLARE_ACCOUNT_ID  target account ID; if not set, read from config file
  CLOUDFLARE_SCRIPT_NAME Worker script name; if not set, read from config file or the name field
                         of wrangler.jsonc / wrangler.json / wrangler.toml
  * Only secrets are synced. Plain text vars (secret: false) are skipped because the [vars] section
    of your wrangler config owns them and would overwrite API-set values on the next deploy.
  * environments map to separate Worker scripts as "<script>-<environment>" (wrangler's [env.X] convention).
    Override the mapping with cloudflare.environments in the config file.

Environment variables (language):
  ENV_SYNC_LANG  display language code (en / ja). Lower priority than --lang flag.

Config file (alternative to environment variables):
  Resolution priority: env var > project config > global config > existing fallback
  global:  ~/.config/env-sync/config.yaml  (respects XDG_CONFIG_HOME)
  project: .env-sync.config.yaml           (current directory)

  Schema (single project):
    vercel:
      token:      <Vercel token>
      project_id: <project ID>
      team_id:    <team ID>
    github:
      token: <GitHub token>
      repo:  <owner/repo>
    cloudflare:
      api_token:  <Cloudflare API token>
      account_id: <account ID>
      script:     <Worker script name (optional; falls back to wrangler config)>
      environments:                 # optional: environment name -> Worker script name
        staging: my-worker-staging

  Schema (monorepo: multiple projects / repos):
    vercel:
      token: <default token (can override per-project)>
      team_id: <default team ID>
      projects:
        - name: app-a
          project_id: <project ID>
        - name: app-b
          project_id: <project ID>
          token: <per-project token (optional)>
          team_id: <per-project team ID (optional)>
    github:
      token: <default token (can override per-repo)>
      repos:
        - name: frontend
          repo: org/frontend
        - name: backend
          repo: org/backend
          token: <per-repo token (optional)>
    cloudflare:
      api_token: <default token (can override per-script)>
      account_id: <default account ID>
      scripts:
        - name: api
          script: my-api-worker
        - name: cron
          script: my-cron-worker
          api_token: <per-script token (optional)>
          account_id: <per-script account ID (optional)>

  * If the global config contains tokens and the file permission is not 0600, a warning is shown.

YAML schema (definition file env-sync.yaml):
  prune: true|false   whether to delete remote variables not in variables (default: false)
                      Vercel: env vars / GitHub: Actions Secrets & Variables /
                      GCP: only Secrets labeled managed-by=env-sync /
                      Cloudflare: Worker secrets on the target scripts
  secret: true|false  whether to register as a secret (default: true)
                      Vercel: true→sensitive / false→plain
                      GitHub: true→Secret / false→Variable
                      Cloudflare: true→secret / false→skipped (managed by wrangler [vars])
  environments: []    array of target environments
                      Vercel: production|preview|development (empty → production,preview)
                      GitHub: named environment name (empty → repo level)
                      Cloudflare: Worker script suffix "<script>-<environment>" (empty → base script)
                      * GitHub named environments must be created in advance
  language: en        display language (en / ja). Lower priority than --lang flag and ENV_SYNC_LANG.
`,

	// ----- 共通同期メッセージ -----
	MsgNoEntries:         "No entries to sync",
	MsgDryRun:            "[dry-run] Not sending",
	MsgNonInteractiveErr: "Non-interactive environment. Use --yes to skip confirmation",
	MsgAborted:           "Aborted",
	MsgCompleted:         "\nDone: success %d / failed %d\n",
	MsgTotalCompleted:    "\nTotal done: success %d / failed %d\n",
	MsgEntriesClassified: "%d entries (%d new / %d update):\n",
	MsgEntriesCount:      "%d entries:\n",
	MsgLabelUpdate:       "update",
	MsgLabelNew:          "new",
	MsgLabelDelete:       "delete",

	// ----- Prune -----
	MsgPruneEntries:        "prune targets (not in the definition file): %d entries:\n",
	MsgPruneSkipWarn:       "⚠ prune: failed to fetch existing entries, skipping deletion: %s\n",
	MsgPruneConfirmNote:    "⚠ prune: %d undefined entries will be DELETED\n",
	MsgPruneExcludeInvalid: "prune_exclude: invalid glob pattern %q",
	MsgRequestCreateFail:   "failed to create request",
	MsgSendFail:            "failed to send",
	MsgRequestFail:         "request failed",

	// ----- Config: ProviderVal -----
	MsgProviderStringOrArray: "provider must be a string or array of strings",

	// ----- Config: AppConfig -----
	MsgConfigReadFail:             "failed to read config file (%s)",
	MsgConfigYAMLFail:             "failed to parse config file YAML (%s)",
	MsgConfigEnvRefUnset:          "environment variable(s) referenced in config are not set or malformed: %s",
	MsgConfigEmptyVarName:         "(empty variable name)",
	MsgConfigPermWarning:          "Warning: %s contains tokens but permission is %04o. Consider running `chmod 0600 %s`\n",
	MsgVercelProjectsNotDefined:   "--vercel-project was specified but vercel.projects is not defined in config",
	MsgVercelProjectNameNotFound:  "specified project name %q is not defined in config",
	MsgVercelProjectNameRequired:  "each entry in vercel.projects must have a name",
	MsgVercelProjectNameDuplicate: "duplicate name %q in vercel.projects",
	MsgVercelProjectIDRequired:    "entry %q in vercel.projects requires project_id",
	MsgGitHubReposNotDefined:      "--github-repo was specified but github.repos is not defined in config",
	MsgGitHubRepoNameNotFound:     "specified repo name %q is not defined in config",
	MsgGitHubRepoNameRequired:     "each entry in github.repos must have a name",
	MsgGitHubRepoNameDuplicate:    "duplicate name %q in github.repos",
	MsgGitHubRepoRepoRequired:     "entry %q in github.repos requires repo",

	MsgCloudflareScriptsNotDefined:   "--cloudflare-script was specified but cloudflare.scripts is not defined in config",
	MsgCloudflareScriptNameNotFound:  "specified cloudflare target name %q is not defined in config",
	MsgCloudflareScriptNameRequired:  "each entry in cloudflare.scripts must have a name",
	MsgCloudflareScriptNameDuplicate: "duplicate name %q in cloudflare.scripts",
	MsgCloudflareScriptRequired:      "entry %q in cloudflare.scripts requires script",

	// ----- Init サブコマンド -----
	MsgInitEnvFileReadFail: "failed to read env file: %s: %s",
	MsgFileExists:          "already exists: %s (use --force to overwrite)",
	MsgInitDefWriteFail:    "failed to write definition file: %s: %s",
	MsgGenerated:           "Generated: %s\n",
	MsgInitKeyCount:        "Keys: %d\n",
	MsgInitKeyListHeader:   "Key list:\n",
	MsgInitSecretNote:      "* Please review the secret field before deploying. Values are not written to the file.",
	MsgInitYAMLHeader: "# Environment variables to register to Vercel / GitHub Actions / GCP / Cloudflare Workers.\n" +
		"#\n" +
		"# Do not write values here (this file is committed to git). Values are read from .env(.production).\n" +
		"# Keys not declared here will not be registered (keys in .env will be skipped with a warning).\n" +
		"#\n" +
		"#   secret: true|false\n" +
		"#           - true  : register as secret (Vercel: sensitive / GitHub: Secret / Cloudflare: Worker Secret)\n" +
		"#           - false : register as plain text (Vercel: plain / GitHub: Variable / Cloudflare: skipped)\n" +
		"#   environments: []  array of target environments\n" +
		"#           Vercel: production|preview|development (empty → production,preview)\n" +
		"#           GitHub: named environment name (empty → repo level)\n" +
		"#           Cloudflare: Worker script suffix \"<script>-<environment>\" (empty → base script)\n" +
		"#\n" +
		"# !! The following is a template generated by init. Review secret before deploying !!\n" +
		"# !! NEXT_PUBLIC_ prefix defaults to secret: false; all others default to secret: true !!\n",
	MsgInitYAMLExample: "  # ---- Example ----\n" +
		"  # NEXT_PUBLIC_API_BASE_URL: { secret: false }\n" +
		"  # DATABASE_URL:             { secret: true }\n" +
		"  # STAGING_KEY:              { secret: true, environments: [production] }\n",

	// ----- Setup サブコマンド -----
	MsgSetupNonInteractive: "setup cannot run in a non-interactive environment (no TTY).\n" +
		"Create the config file manually or use the following example:\n" +
		"  mkdir -p ~/.config/env-sync\n" +
		"  cat > ~/.config/env-sync/config.yaml <<'EOF'\n" +
		"  vercel:\n" +
		"    token: ${VERCEL_TOKEN}\n" +
		"    project_id: <project ID>\n" +
		"  github:\n" +
		"    token: ${GITHUB_TOKEN}\n" +
		"    repo: <owner/repo>\n" +
		"  EOF\n" +
		"  chmod 0600 ~/.config/env-sync/config.yaml",
	MsgSetupHomeDirFail:       "could not determine home directory",
	MsgSetupDirCreateFail:     "failed to create directory: %s: %s",
	MsgSetupConfigWriteFail:   "failed to write config file: %s: %s",
	MsgSetupConfigChmodFail:   "failed to set config file permission: %s: %s",
	MsgSetupInputReadFail:     "failed to read input",
	MsgSetupTokenNote:         "File contains tokens. Permission is set to 0600.",
	MsgSetupGitignoreNote:     "* It is recommended to add this file to .gitignore.",
	MsgSetupAskVercel:         "Configure Vercel? (Y/n): ",
	MsgSetupVercelProjectID:   "Vercel project_id: ",
	MsgSetupVercelTeamID:      "Vercel team_id (optional, press Enter to skip): ",
	MsgSetupVercelTokenEnvRef: "Write Vercel token as ${VERCEL_TOKEN} env reference? (recommended) (Y/n): ",
	MsgSetupVercelTokenPlain:  "Vercel token (will be written in plaintext): ",
	MsgSetupAskGitHub:         "Configure GitHub? (Y/n): ",
	MsgSetupGitHubRepo:        "GitHub repo (owner/repo format): ",
	MsgSetupGitHubTokenEnvRef: "Write GitHub token as ${GITHUB_TOKEN} env reference? (recommended) (Y/n): ",
	MsgSetupGitHubTokenPlain:  "GitHub token (will be written in plaintext): ",

	MsgSetupAskCloudflare:       "Configure Cloudflare Workers? (Y/n): ",
	MsgSetupCloudflareAccountID: "Cloudflare account_id: ",
	MsgSetupCloudflareScript:    "Cloudflare Worker script name (optional, press Enter to read from wrangler config): ",
	MsgSetupCloudflareTokenEnvRef: "Write Cloudflare token as ${CLOUDFLARE_API_TOKEN} env reference? " +
		"(recommended) (Y/n): ",
	MsgSetupCloudflareTokenPlain: "Cloudflare API token (will be written in plaintext): ",

	MsgSetupYAMLHeader: "# env-sync auth config\n" +
		"# It is recommended to add this file to .gitignore.\n" +
		"# Add projects[] / repos[] / scripts[] (for monorepo multi-target) manually.\n",

	// ----- Vercel Provider -----
	MsgVercelTokenMissing:           "VERCEL_TOKEN is not set (set VERCEL_TOKEN env var or vercel.token in config file)",
	MsgVercelTokenMissingProject:    "VERCEL_TOKEN is not set (project %q: set VERCEL_TOKEN env var or token in config file)",
	MsgVercelTokenSkipProject:       "✗ project %q: VERCEL_TOKEN is not set (skipping this target and continuing)\n",
	MsgVercelExistingKeysFetchWarn:  "Warning: failed to fetch existing keys, skipping new/update classification: %s\n",
	MsgVercelTargetProject:          "Target project: %s  (env: %s, def: %s)\n",
	MsgVercelEntriesUpsert:          "%d entries (existing will be overwritten by upsert):\n",
	MsgVercelConfirmMulti:           "Register the above to %d Vercel projects (existing will be overwritten). Continue? (y/N) ",
	MsgVercelConfirmSingle:          "Register the above to Vercel (existing will be overwritten). Continue? (y/N) ",
	MsgVercelProjectSeparator:       "\n--- project: %s ---\n",
	MsgVercelURLBuildFailOut:        "✗ failed to build URL: %s\n",
	MsgVercelRequestCreateFailOut:   "✗ %s -> failed to create request: %s\n",
	MsgVercelSendFailOut:            "✗ %s -> failed to send: %s\n",
	MsgVercelProjectJSONReadFail:    "failed to read .vercel/project.json: %s",
	MsgVercelProjectJSONParseFail:   "failed to parse .vercel/project.json: %s",
	MsgVercelProjectIDMissing:       "VERCEL_PROJECT_ID is not set and .vercel/project.json not found (run vercel link first or specify the project ID)",
	MsgVercelProjectNotDefined:      "%s: vercel_project is specified but vercel.projects is not defined in config (use vercel_project together with vercel.projects[])",
	MsgVercelProjectInvalidConfig:   "%s: vercel_project %q does not exist in config vercel.projects (defined: %s)",
	MsgVercelCustomEnvNotFound:      "custom environment %q does not exist in project %s (available: %s)",
	MsgVercelCustomEnvFetchFail:     "failed to fetch custom environments",
	MsgVercelCustomEnvParseFail:     "failed to parse custom environments response",
	MsgVercelCustomEnvNoneAvailable: "(none — please create a Custom Environment in the Vercel dashboard first)",
	MsgVercelURLBuildFailInternal:   "failed to build URL",
	MsgVercelExistingKeyFetchFail:   "failed to fetch existing keys",
	MsgVercelExistingKeyParseFail:   "failed to parse existing keys response",

	// ----- GitHub Provider -----
	MsgGitHubTokenMissing:          "GITHUB_TOKEN is not set (set GITHUB_TOKEN env var or github.token in config file)",
	MsgGitHubTokenMissingRepo:      "GITHUB_TOKEN is not set (repo %q: set GITHUB_TOKEN env var or token in config file)",
	MsgGitHubTokenSkipRepo:         "✗ repo %q: GITHUB_TOKEN is not set (skipping this target and continuing)\n",
	MsgGitHubExistingCheckWarn:     "Warning: failed to check existing entries, skipping new/update classification: %s\n",
	MsgGitHubTargetRepo:            "Target repo: %s/%s\n",
	MsgGitHubConfirmMulti:          "Register the above to %d GitHub repositories (existing will be overwritten). Continue? (y/N) ",
	MsgGitHubConfirmSingle:         "Register the above to GitHub. Continue? (y/N) ",
	MsgGitHubRepoSeparator:         "\n--- repo: %s/%s ---\n",
	MsgGitHubPublicKeyFetchFailOut: "✗ %s -> failed to fetch public key: %s\n",
	MsgGitHubEncryptFailOut:        "✗ %s -> failed to encrypt: %s\n",
	MsgGitHubExistCheckFailOut:     "✗ %s (env: %s) -> failed to check existence: %s\n",
	MsgGitHubExistCheckTaskFail:    "%s (env: %s): existence check failed",
	MsgGitHubRepoFormatInvalid:     "invalid repository format (must be owner/repo): %q",
	MsgGitHubRepoEnvInvalid:        "invalid GITHUB_REPO format (must be owner/repo)",
	MsgGitHubRepoRequired:          "GITHUB_REPO is required (git remote origin is not GitHub or git is unavailable)",
	MsgSealedBoxEncryptFail:        "sealed box encryption failed",
	MsgPublicKeyFetchFail:          "failed to fetch public key",
	MsgPublicKeyParseFail:          "failed to parse public key response",
	MsgPublicKeyDecodeFail:         "failed to base64-decode public key",
	MsgPublicKeyLengthInvalid:      "invalid public key length (%d bytes, expected 32)",
	MsgSecretExistCheckFail:        "failed to check secret existence: %s",
	MsgVariableExistCheckFail:      "failed to check variable existence: %s",

	// ----- GCP Provider -----
	MsgGCPProjectIDMissing:      "GCP_PROJECT_ID is not set",
	MsgGCPSkipNotSecret:         "⚠ %s: secret=false, skipping (Secret Manager is for secrets only)\n",
	MsgGCPTargetProject:         "Target project: %s\n",
	MsgGCPLabelsNone:            "(no labels)",
	MsgGCPConfirm:               "Sync the above to GCP Secret Manager (add as new version). Continue? (y/N) ",
	MsgGCPClientCreateFail:      "failed to create Secret Manager client: %s",
	MsgGCPSecretGetFail:         "failed to get Secret: %s",
	MsgGCPSecretCreateFail:      "failed to create Secret: %s",
	MsgGCPSecretLabelUpdateFail: "failed to update Secret labels: %s",
	MsgGCPSecretVersionAddFail:  "failed to add Secret version: %s",

	// ----- Cloudflare Provider -----
	MsgCloudflareTokenMissing:       "CLOUDFLARE_API_TOKEN is not set (set CLOUDFLARE_API_TOKEN env var or cloudflare.api_token in config file)",
	MsgCloudflareTokenMissingScript: "CLOUDFLARE_API_TOKEN is not set (target %q: set CLOUDFLARE_API_TOKEN env var or api_token in config file)",
	MsgCloudflareTokenSkipScript:    "✗ target %q: CLOUDFLARE_API_TOKEN is not set (skipping this target and continuing)\n",
	MsgCloudflareAccountIDMissing:   "CLOUDFLARE_ACCOUNT_ID is not set (set CLOUDFLARE_ACCOUNT_ID env var or cloudflare.account_id in config file)",
	MsgCloudflareScriptMissing: "Worker script name could not be resolved " +
		"(set CLOUDFLARE_SCRIPT_NAME env var, cloudflare.script in config file, or name in wrangler.jsonc / wrangler.toml)",
	MsgCloudflareSkipNotSecret: "⚠ %s: secret=false, skipping (Cloudflare Workers provider syncs secrets only)\n",
	MsgCloudflareVarsNote: "* Plain text vars (secret: false) are not synced: they are owned by the [vars] section of your\n" +
		"  wrangler config and would be overwritten on the next `wrangler deploy`. Secrets are preserved across deploys.\n",
	MsgCloudflareTargetScript:          "Target Worker: %s  (env: %s, def: %s)\n",
	MsgCloudflareConfirmSingle:         "Register the above to Cloudflare Workers (existing will be overwritten). Continue? (y/N) ",
	MsgCloudflareConfirmMulti:          "Register the above to %d Cloudflare Workers targets (existing will be overwritten). Continue? (y/N) ",
	MsgCloudflareScriptSeparator:       "\n--- target: %s ---\n",
	MsgCloudflareExistingKeysFetchWarn: "Warning: failed to fetch existing secrets, skipping new/update classification: %s\n",
	MsgCloudflareSecretsFetchFail:      "failed to fetch existing secrets",
	MsgCloudflareSecretsParseFail:      "failed to parse existing secrets response",
	MsgCloudflareURLBuildFailOut:       "✗ failed to build URL: %s\n",
	MsgCloudflareURLBuildFailInternal:  "failed to build URL",
	MsgCloudflareRequestCreateFailOut:  "✗ %s -> failed to create request: %s\n",
	MsgCloudflareSendFailOut:           "✗ %s -> failed to send: %s\n",
	MsgCloudflareWranglerReadFail:      "failed to read wrangler config: %s: %s",

	// ----- Validate サブコマンド -----
	MsgValidateHeader:               "=== validate: %s ===\n",
	MsgValidateProviderUnsupported:  "  [skip] %s: validate not supported\n",
	MsgValidateSourceEnv:            "env var",
	MsgValidateSourceConfig:         "config file",
	MsgValidateSourceProjectJSON:    ".vercel/project.json",
	MsgValidateSourceGitRemote:      "git remote",
	MsgValidateSourceUnset:          "(unset)",
	MsgValidateTokenMasked:          "[set] (source: %s)",
	MsgValidateTokenUnset:           "[unset]",
	MsgValidateHTTPStatus:           "HTTP %d",
	MsgValidateOK:                   "OK",
	MsgValidateTokenUnsetSkip:       "  token is not set, skipping API check\n",
	MsgValidateProjectIDUnsetSkip:   "  projectId is not set, skipping API check\n",
	MsgValidateRepoUnresolvableSkip: "  repo could not be resolved, skipping API check\n",
	MsgValidateVercelCause404:       "  Possible cause: teamId not set, or projectId mismatch\n",
	MsgValidateVercelCause401:       "  Possible cause: token is invalid\n",
	MsgValidateVercelCause403:       "  Possible cause: token lacks required scope\n",
	MsgValidateGitHubCause404:       "  Possible cause: repo does not exist or token lacks access to private repo\n",
	MsgValidateGitHubCause401:       "  Possible cause: token is invalid\n",
	MsgValidateGitHubCause403:       "  Possible cause: token lacks required scope or rate limit exceeded\n",
	MsgValidateResult:               "validate: success %d / failed %d\n",
	MsgValidateVercelProjectID:      "  projectId : %s (source: %s)\n",
	MsgValidateVercelTeamID:         "  teamId    : %s (source: %s)\n",
	MsgValidateGitHubRepo:           "  repo      : %s (source: %s)\n",
	MsgValidateSourceWrangler:       "wrangler config",
	MsgValidateCloudflareAccountID:  "  accountId : %s (source: %s)\n",
	MsgValidateCloudflareScript:     "  script    : %s (source: %s)\n",
	MsgValidateAccountIDUnsetSkip:   "  accountId is not set, skipping API check\n",
	MsgValidateScriptUnsetSkip:      "  script name could not be resolved, skipping API check\n",
	MsgValidateCloudflareCause404:   "  Possible cause: the Worker script does not exist, or accountId is wrong\n",
	MsgValidateCloudflareCause401:   "  Possible cause: API token is invalid\n",
	MsgValidateCloudflareCause403:   "  Possible cause: API token lacks the Workers Scripts:Edit permission\n",

	// ----- Sync / Entry 解決 -----
	MsgDefaultsProviderInvalid: "defaults.provider: invalid provider value %q (must be one of: %s)",
	MsgDefaultsProviderEmpty:   "defaults.provider: empty array specified (must be one of: %s)",
	MsgVarProviderEmpty:        "%s: provider: empty array specified (must be one of: %s)",
	MsgVarProviderBlank:        "%s: provider is empty or whitespace-only (must be one of: %s)",
	MsgVarProviderInvalid:      "%s: invalid provider value %q (must be one of: %s)",
}
