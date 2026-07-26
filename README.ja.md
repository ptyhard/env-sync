# env-sync

[English](README.md) | 日本語

定義ファイル `env-sync.yaml` で宣言した環境変数を **Vercel** / **GitHub Actions** / **GCP Secret Manager** / **Cloudflare Workers** へ一括登録（同期）する Go 製 CLI。
1 つの定義ファイルから、変数ごとに同期先を選んで複数のプロバイダーへまとめて反映できる。

- **Vercel**: REST API (`POST /v10/projects/{id}/env?upsert=true`) を使うため、再実行すると既存の変数は **更新（upsert）** される。
- **GitHub Actions**: Secrets（sealed box 暗号化）と Variables（平文）の両方に対応する。
- **GCP Secret Manager**: 実行ごとに Secret のバージョンを追加する。`secret: true` のエントリのみ同期する。
- **Cloudflare Workers**: REST API で Worker Secrets を登録する。`secret: true` のエントリのみ同期する（[Cloudflare Workers へ同期](#5-cloudflare-workers-へ同期)を参照）。

## 仕組み

- **secret / environments は `env-sync.yaml` で明示的に宣言**する（キー名のヒューリスティックに頼らない）。
- **値は `env-sync.yaml` には書かない**（git にコミットされるため）。値は `.env(.production)` から取得する。
- 定義に無いキーは登録されない（`.env` にあっても警告のうえスキップ）。
- 定義にあるが `.env` に値が無いキーも警告のうえスキップ。

## インストール

### Homebrew（macOS / Linux）

```bash
brew install ptyhard/tap/env-sync
```

### go install

```bash
go install github.com/ptyhard/env-sync/cmd/env-sync@latest
```

### バイナリを直接ダウンロード

[GitHub Releases](https://github.com/ptyhard/env-sync/releases) から最新バイナリをダウンロードしてください。
darwin/amd64、darwin/arm64、linux/amd64、linux/arm64 向けアーカイブが提供されます。

### ソースからビルド

```bash
go build -o env-sync ./cmd/env-sync
```

## クイックスタート

```bash
# 1. 認証情報 config を対話生成（Vercel token / project_id / GitHub token / repo を設定）
env-sync setup

# 2. 既存の .env から定義ファイルの雛形を生成
env-sync init

# 3. 生成された env-sync.yaml の secret / environments / provider を見直す

# 4. 送信せずに登録予定を確認（dry-run）
VERCEL_TOKEN=xxxxx env-sync --env .env.production --dry-run

# 5. 本番投入（更新がある場合のみ y/N 確認）
VERCEL_TOKEN=xxxxx env-sync --env .env.production
```

## 1. setup で認証情報 config を生成

認証情報 config ファイル（`.env-sync.config.yaml` または `~/.config/env-sync/config.yaml`）を対話プロンプトで生成します。

```bash
# プロジェクト config（.env-sync.config.yaml）を生成
env-sync setup

# global config（~/.config/env-sync/config.yaml）を生成
env-sync setup --global

# 既存ファイルを上書き（--force なしでは上書きを拒否してエラー）
env-sync setup --force
```

- Vercel / GitHub それぞれについて、設定するか・project_id・repo・token 入力方法を順に質問します
- **token はデフォルトで `${VERCEL_TOKEN}` / `${GITHUB_TOKEN}` の環境変数参照形式**で書き出されます（平文トークンをファイルに書かずに済む推奨形式）
- 生 token を直接書く場合、または `--global` 出力時はファイルを **0600** で作成し、その旨を案内します
- 非対話環境（TTY なし）では手書き方法を案内してエラーで停止します

> **注意**: このファイルをコミットしないよう `.gitignore` に `.env-sync.config.yaml` を追記することを推奨します。

## 2. init で変数定義の雛形を生成

既存の `.env` を読み込み、値を含まない `env-sync.yaml` の雛形を自動生成します。

```bash
# 基本（.env から env-sync.yaml を生成）
env-sync init

# 別ファイルを指定
env-sync init --env .env.production

# 出力先を指定
env-sync init --env .env.production --def env-sync.production.yaml

# 既存ファイルを上書き（--force なしでは上書きを拒否してエラー）
env-sync init --env .env.production --force
```

- `NEXT_PUBLIC_` プレフィックスのキーは `secret: false`、それ以外は `secret: true` を初期値として設定します
- これはあくまで**雛形**です。secret は投入前に必ず見直してください
- **値は一切書き込まれません**（`.env` の値が yaml に混入することはありません）
- 既存の `env-sync.yaml` がある場合、`--force` なしでは上書きを拒否してエラーで終了します

## 3. Vercel へ同期

### 事前準備

```bash
# プロジェクトをリンク（初回のみ。.vercel/project.json が生成される）
vercel link

# アクセストークンを発行
#   https://vercel.com/account/tokens
```

### 同期

```bash
# dry-run（送信せず新規/更新の区別を含む登録予定一覧を表示。値は表示されない）
VERCEL_TOKEN=xxxxx env-sync --env .env.production --dry-run

# 本番投入（新規/更新を分類して表示。更新がある場合のみ y/N 確認）
VERCEL_TOKEN=xxxxx env-sync --env .env.production

# 更新(上書き)確認をスキップ（CI など）
VERCEL_TOKEN=xxxxx env-sync --env .env.production --yes
```

送信前に provider へ問い合わせ、各キーを「`+ KEY [新規]`」または「`⟳ KEY [更新]`」として分類表示します。
**更新（上書き）対象がある場合のみ** `y/N` の確認プロンプトが表示されます。新規登録のみなら確認なしで送信します。
`--yes`（`-y`）を付けると確認をスキップできます。非対話環境（TTY なし）で更新対象があり `--yes` がない場合は安全のためエラーで停止します。
`--dry-run` 時もトークンが設定されていれば新規/更新の分類を表示します（送信はしません）。

## 4. GitHub Actions へ同期

`--provider github` を指定すると GitHub Actions の Secrets/Variables に同期します。

```bash
# dry-run（送信せず新規/更新の区別を含む登録予定一覧を表示。値は表示されない）
GITHUB_REPO=owner/repo env-sync --provider github --env .env.production --dry-run

# 本番投入（リポジトリレベル。更新がある場合のみ確認プロンプトが表示される）
GITHUB_TOKEN=xxxxx GITHUB_REPO=owner/repo env-sync --provider github --env .env.production

# named environment に登録（env-sync.yaml の environments フィールドで指定）
# environments: [production] を yaml に書けばその environment に登録される
GITHUB_TOKEN=xxxxx env-sync --provider github --env .env.production

# 更新(上書き)確認をスキップ（CI など）
GITHUB_TOKEN=xxxxx env-sync --provider github --yes --env .env.production
```

`GITHUB_REPO` を省略すると `git remote origin` から `owner/repo` を自動取得します。

### `env-sync.yaml` での GitHub 向け設定例

```yaml
defaults:
  secret: true

variables:
  DATABASE_URL:
    secret: true                      # repo レベルの Secret
  PUBLIC_FLAG:
    secret: false                     # repo レベルの Variable
  API_KEY_PROD:
    secret: true
    environments: [production]        # production environment の Secret
  FEATURE_FLAG:
    secret: false
    environments: [production, staging]  # production, staging の Variable
```

> **注意**: `environments` に指定した named environment は GitHub 上で事前に作成が必要です。存在しない environment 名を指定するとエラーになります。

### セキュリティ

- `secret: true` の変数は **GitHub の公開鍵で sealed box 暗号化**してから PUT します。平文は送信しません。
- 登録対象の一覧表示・確認プロンプト・成否ログのいずれにも **値は出力されません**。`--dry-run` でも同様。
- 公開鍵の長さ（32 バイト）を検証し、不正な鍵では処理を中止します。
- 公開鍵キャッシュは envScope（environment）ごとに管理します（スコープで鍵が異なるため）。

## 5. Cloudflare Workers へ同期

`--provider cloudflare` を指定すると、Cloudflare REST API
(`PUT /accounts/{account_id}/workers/scripts/{script_name}/secrets`) 経由で **Worker Secrets** を同期します。

### 事前準備

- **Workers Scripts:Edit** 権限を持つ API トークン（https://dash.cloudflare.com/profile/api-tokens）
- アカウント ID（Workers & Pages → 概要、または `wrangler whoami`）
- 対象の Worker スクリプトが既にデプロイ済みであること（env-sync は Worker を新規作成しません）

### 同期

```bash
# dry-run（値は表示されない）
CLOUDFLARE_API_TOKEN=xxxxx CLOUDFLARE_ACCOUNT_ID=yyyyy \
  env-sync --provider cloudflare --env .env.production --dry-run

# 本番反映（更新がある場合のみ y/N 確認）
CLOUDFLARE_API_TOKEN=xxxxx CLOUDFLARE_ACCOUNT_ID=yyyyy \
  env-sync --provider cloudflare --env .env.production

# 更新確認をスキップ（CI 等）
CLOUDFLARE_API_TOKEN=xxxxx CLOUDFLARE_ACCOUNT_ID=yyyyy \
  env-sync --provider cloudflare --env .env.production --yes
```

### シークレットのみ同期（平文 vars はスキップ）

Cloudflare Workers の環境変数には 2 種類あり、env-sync が同期するのは **Secrets のみ**です。

| 宣言 | 挙動 |
|------|------|
| `secret: true` | Worker Secret として登録。**`wrangler deploy` をまたいで保持される** |
| `secret: false` | **警告を出してスキップ** |

平文 vars は wrangler 設定の `[vars]` セクションが管理しており、API やダッシュボードで設定した値は
次の `wrangler deploy` で上書きされて消えます。同期しても黙って失われるため、平文 vars は
`wrangler.jsonc` / `wrangler.toml` 側で管理し、env-sync はシークレットのみを扱います。

### `environments` は別の Worker スクリプトに対応する

wrangler は `[env.staging]` を `<スクリプト名>-staging` という **別の Worker** としてデプロイします。
env-sync もこの慣習に従い、宣言した環境ごとに対応する Worker スクリプトへ書き込みます。

```yaml
# env-sync.yaml（スクリプト名が "my-worker" に解決される場合）
variables:
  API_KEY:      { secret: true, provider: cloudflare }
  DATABASE_URL: { secret: true, provider: cloudflare, environments: [production, staging] }
```

| 宣言 | 送信先 Worker スクリプト |
|------|------------------------|
| `environments` 省略 | `my-worker` |
| `environments: [staging]` | `my-worker-staging` |
| `environments: [production, staging]` | `my-worker-production` と `my-worker-staging` |

本番 Worker が `my-worker-production` ではなくベーススクリプトの場合は、認証情報 config で対応を上書きできます。

```yaml
# .env-sync.config.yaml
cloudflare:
  api_token: ${CLOUDFLARE_API_TOKEN}
  account_id: <アカウント ID>
  environments:
    production: my-worker          # production はベーススクリプトへ書き込む
    staging: my-worker-staging
```

`cloudflare.environments` に無い環境名は従来どおり `<スクリプト名>-<環境名>` の規約で解決されます。

### Worker スクリプト名の解決順

解決優先順位（高い順）:

1. 環境変数 `CLOUDFLARE_SCRIPT_NAME`
2. 認証情報 config の `cloudflare.script`
3. カレントディレクトリの `wrangler.jsonc` → `wrangler.json` → `wrangler.toml` の `name` フィールド

いずれでも解決できない場合はエラーで停止します。

### 複数 Worker（モノレポ）

`cloudflare.scripts[]` を定義すると 1 回の実行で複数の Worker へ同期でき、`--cloudflare-script` で絞り込めます。

```yaml
# .env-sync.config.yaml
cloudflare:
  api_token: ${CLOUDFLARE_API_TOKEN}   # per-script で省略した場合のフォールバック
  account_id: <アカウント ID>           # per-script で省略した場合のフォールバック
  scripts:
    - name: api                        # --cloudflare-script で絞り込むための識別名
      script: my-api-worker
    - name: cron
      script: my-cron-worker
      api_token: ${CRON_CF_TOKEN}      # この Worker だけ別トークン
```

```bash
# 定義済みの全 Worker へ同期
env-sync --provider cloudflare --env .env.production

# 特定の Worker のみに絞る
env-sync --provider cloudflare --env .env.production --cloudflare-script cron
```

### 設定の診断

```bash
# token / accountId / スクリプト名の設定と API 到達確認（読み取り専用）
env-sync validate --provider cloudflare
```

## 6. 複数プロバイダーを混在させる

変数ごとに `provider` を指定すると、1 つの `env-sync.yaml` から Vercel と GitHub Actions の両方へ同時に同期できます。

```yaml
defaults:
  secret: true
  # defaults.provider を指定するとすべての変数のデフォルト同期先を変更できる
  # provider: github

variables:
  DATABASE_URL:
    secret: true
    provider: vercel            # Vercel のみに同期

  GITHUB_ACTIONS_TOKEN:
    secret: true
    provider: github            # GitHub Actions のみに同期

  SHARED_SECRET:
    secret: true
    provider: [vercel, github]  # 両方に同期

  PUBLIC_API_URL:
    secret: false               # provider 未指定 → --provider フラグのデフォルト（vercel）
```

```bash
# トークンを両方渡せば 1 回の実行で Vercel / GitHub の両方へ振り分けて同期される
VERCEL_TOKEN=xxxxx GITHUB_TOKEN=yyyyy env-sync --env .env.production
```

解決優先順位（高い順）: **変数個別の `provider`** → **`defaults.provider`** → **CLI `--provider` フラグ**（デフォルト `vercel`）

不正な値（`vercel` / `github` 以外）を指定するとエラーで中止します。`--dry-run` では各変数の `providers` 列で振り分け先を確認できます。

## 7. config ファイルで認証情報・ID を管理する

環境変数の代わりに YAML ファイルでトークンや ID を管理できます。毎回 `VERCEL_TOKEN=...` を渡す手間を省けます。

### config ファイルの場所

| 種別 | パス |
|------|------|
| global | `~/.config/env-sync/config.yaml`（`XDG_CONFIG_HOME` を尊重） |
| project | `.env-sync.config.yaml`（カレントディレクトリ） |

どちらのファイルも存在しない場合は従来通り環境変数＋既存フォールバックのみで動作します（後方互換）。

### 解決優先順位

```
環境変数 > project config > global config > 既存フォールバック（.vercel/project.json / git remote）
```

環境変数が設定されていれば常に優先されます。

### スキーマ

```yaml
vercel:
  token:      <Vercel アクセストークン>
  project_id: <プロジェクト ID>
  team_id:    <チーム(Org) ID>
github:
  token: <GitHub アクセストークン>
  repo:  <owner/repo 形式のリポジトリ名>
cloudflare:
  api_token:  <Cloudflare API トークン>
  account_id: <アカウント ID>
  script:     <Worker スクリプト名（任意。省略時は wrangler 設定から取得）>
  environments:                    # 任意: 環境名 -> Worker スクリプト名
    staging: my-worker-staging
```

### 複数 Vercel プロジェクト / 複数 GitHub リポジトリ（モノレポ）

turborepo などのモノレポで 1 リポジトリに複数アプリがある場合、`vercel.projects` / `github.repos` の配列で複数の同期先を定義できます。1 回の実行で定義済みの全ターゲットへ環境変数を配れます。

```yaml
vercel:
  token: <共通トークン>            # 各プロジェクトで token 省略時のフォールバック
  team_id: team_xxxxxxxx          # 各プロジェクトで team_id 省略時のフォールバック
  projects:
    - name: web                   # --vercel-project で絞り込む際の識別名
      project_id: prj_web
    - name: admin
      project_id: prj_admin
      team_id: team_yyyyyyyy      # このプロジェクトだけ別チーム
      token: ${ADMIN_VERCEL_TOKEN} # このプロジェクトだけ別トークン

github:
  token: <共通トークン>            # 各リポジトリで token 省略時のフォールバック
  repos:
    - name: web
      repo: myorg/web
    - name: infra
      repo: myorg/infra
      token: ${INFRA_GH_TOKEN}    # このリポジトリだけ別トークン
```

- 各ターゲットの `token` / `team_id` を省略すると、トップレベルの `vercel.token` / `vercel.team_id` / `github.token`（さらに環境変数）にフォールバックします。
- `token` などには `${VAR}` / `${VAR:-default}` 参照も使えます。
- **複数定義時は各ターゲットの `project_id` / `repo` が必須**です（`.vercel/project.json` / git remote フォールバックは単一定義時のみ有効）。

```bash
# 定義済みの全 Vercel プロジェクトへ同期
env-sync --env .env.production

# 特定のプロジェクト / リポジトリだけに絞り込む
env-sync --env .env.production --vercel-project admin
env-sync --provider github --env .env.production --github-repo infra
```

実行時は各ターゲットごとに「対象プロジェクト / 対象リポジトリ」見出しと新規/更新の分類一覧が表示されます。あるターゲットで失敗しても残りのターゲットは継続して試行し、いずれかで失敗があれば終了コード 1 で終わります。

> **後方互換**: `projects` / `repos` を定義しない場合は、従来どおり単一の `vercel.project_id` / `github.repo`（および環境変数・`.vercel/project.json` / git remote フォールバック）で 1 ターゲットへ同期します。

#### 変数ごとに送信先 Vercel プロジェクトを絞り込む（`vercel_project`）

`--vercel-project` フラグは実行全体を 1 プロジェクトに絞りますが、`env-sync.yaml` の各変数で `vercel_project` を指定すると、**変数単位**で送信先 Vercel プロジェクトを `name` で絞り込めます（`vercel.projects[]` の定義が前提）。

```yaml
# env-sync.yaml
defaults:
  secret: true
  # vercel_project: web        # defaults に書くと全変数のデフォルト送信先になる

variables:
  WEB_API_URL:   { provider: vercel, vercel_project: web }          # web にのみ送る
  ADMIN_API_URL: { provider: vercel, vercel_project: admin }        # admin にのみ送る
  SHARED_DB_URL: { provider: vercel, vercel_project: [web, admin] } # web と admin に送る
  COMMON_KEY:    { provider: vercel }                               # vercel_project 未指定 → 全プロジェクトへ（従来どおり）
```

- **解決優先順位**: 変数個別 `vercel_project` > `defaults.vercel_project`。未指定の変数は解決済みの全 Vercel ターゲットへ送ります（後方互換）。
- **CLI `--vercel-project` との併用は AND**: CLI で絞り込んだターゲット集合に対し、さらに変数ごとの `vercel_project` でフィルタされます。
- `vercel_project` に指定する `name` は `vercel.projects[].name` を指します。存在しない `name` を指定するとエラーで停止します。
- `vercel.projects[]` を定義していない単一解決モードでは `vercel_project` は指定できません（エラーになります）。

### 使用例

```bash
# global config (~/.config/env-sync/config.yaml) を作成
mkdir -p ~/.config/env-sync
cat > ~/.config/env-sync/config.yaml <<'EOF'
vercel:
  token: your-vercel-token
github:
  token: your-github-token
EOF
chmod 0600 ~/.config/env-sync/config.yaml

# project config でプロジェクト固有の ID を上書き
cat > .env-sync.config.yaml <<'EOF'
vercel:
  project_id: prj_xxxxxxxx
  team_id: team_xxxxxxxx
github:
  repo: myorg/myrepo
EOF

# 環境変数なしで実行
env-sync --env .env.production
```

### `--environments` で書き込み先環境を限定する

`env-sync.yaml` 1 本のまま、同期実行時に `--environments` で書き込み先を絞り込めます。

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
# production のみに同期（environments に production が含まれない変数はスキップ）
env-sync --env .env.production --def env-sync.yaml --environments production

# staging と preview のみに同期
env-sync --env .env.staging --def env-sync.yaml --environments staging,preview

# prune との組み合わせ（削除スコープも staging に限定）
env-sync --env .env.staging --def env-sync.yaml --environments staging --prune
```

- 変数の `environments` と `--environments` の積集合が空になる変数はスキップされます（stderr に警告を出力）。
- 定義ファイルで `environments` を**宣言していない**変数も `--environments` 指定時はスキップされます（積集合を取れる宣言環境が存在しないため）。
- **全変数がスキップされた場合**、どのプロバイダーへの送信も行われず、prune も実行されずに exit 0 で正常終了します。
- `--environments` に指定できる名前は、定義ファイルの `environments` フィールドに存在する値、または標準の `production` / `preview` / `development` です。不正な名前が含まれる場合はエラーで終了（exit 1）します。
- 環境名は**大文字小文字を区別します** — `env-sync.yaml` の宣言と完全に一致する必要があります。
- **GCP**: GCP Secret に環境スコープの概念はありません。フィルタを通過した変数は引き続き同期され、`managed-by=env-sync` ラベルが付与・更新される副作用があります。`--prune` と組み合わせた場合、GCP の prune スコープは `--environments` で絞られません（`managed-by=env-sync` ラベル付き Secret が全件 prune 対象になります）。

### 環境変数参照 `${VAR}` / `${VAR:-default}`

config の値に環境変数参照を書くことができます（平文トークンを config ファイルに書きたくない場合に便利です）。

| 記法 | 動作 |
|------|------|
| `${VAR}` | 環境変数 `VAR` の値に展開。`VAR` が未設定（空含む）の場合はエラーで中止 |
| `${VAR:-default}` | `VAR` が設定されていれば `VAR` の値、未設定または空文字なら `default` を使用 |

```yaml
vercel:
  token: ${MY_VERCEL_TOKEN}          # MY_VERCEL_TOKEN 未設定ならエラー
  project_id: ${V_PID:-prj_default}  # V_PID 未設定なら prj_default
github:
  token: ${GH_TOKEN}
  repo: ${GH_REPO:-myorg/myrepo}
```

> **注意**: 既存の優先順位「環境変数 > project config > global config」は維持されます。
> config に `token: ${VERCEL_TOKEN}` と書いても、`VERCEL_TOKEN` 環境変数が設定されていれば直接 env が優先されるため実質同じ値になります。
> **別名変数**（例: `${MY_VERCEL_TOKEN}`）を使うケースで本機能が活きます。

> **セキュリティ**: global config にトークンが含まれていてファイルパーミッションが `0600` でない場合、実行時に stderr へ警告を出力します。`chmod 0600 ~/.config/env-sync/config.yaml` で修正してください。暗号化保存・キーチェーン連携は対象外です（将来検討）。

## 定義ファイル `env-sync.yaml`

```yaml
defaults:
  secret: true

variables:
  NEXT_PUBLIC_FIREBASE_API_KEY: { secret: false }
  DATABASE_URL: { secret: true, environments: [production] }
  API_SECRET: { secret: true }
```

### スキーマ

| フィールド | 型 | 説明 |
|---|---|---|
| `prune` | `bool`（トップレベル） | `true`: 同期時に `variables` に無いリモートの変数を削除（デフォルト `false`）。`--prune` フラグでも有効化可。詳細は[定義に無い変数の削除（prune）](#定義に無い変数の削除prune)を参照 |
| `prune_exclude` | `[]string`（トップレベル） | prune で削除しないキー名の glob パターン一覧（例: `[BLOB_*, SENTRY_DSN]`）。大文字小文字は区別しない |
| `secret` | `bool` | `true`（デフォルト）: シークレット登録 / `false`: 平文登録 |
| `environments` | `[]string` | 登録先環境の配列。省略すると `defaults.environments` を継承 |
| `provider` | `string` または `[]string` | 同期先プロバイダー。省略すると `defaults.provider` → CLI `--provider` フラグを使用 |
| `vercel_project` | `string` または `[]string` | この変数の送信先 Vercel プロジェクト名（`vercel.projects[].name`）。省略すると `defaults.vercel_project` → 全 Vercel ターゲット。詳細は[変数ごとに送信先 Vercel プロジェクトを絞り込む](#変数ごとに送信先-vercel-プロジェクトを絞り込むvercel_project)を参照 |

#### Vercel における各フィールドの意味

| `secret` | Vercel type | 説明 |
|---|---|---|
| `true` | `sensitive` | 保存後は値を読めないシークレット向け |
| `false` | `plain` | 暗号化なしの平文 |

`environments` には `production` / `preview` / `development` を指定。省略時（空）は `production` と `preview` をデフォルト適用。

`environments` には Custom Environment の slug（例 `staging`）も指定可。Custom Environment は Vercel 側で事前作成が必要で、存在しない slug はエラー。標準環境と混在指定も可能（例 `[production, staging]`）。

#### GitHub Actions における各フィールドの意味

| `secret` | 登録先 | 説明 |
|---|---|---|
| `true` | GitHub Actions Secrets | sealed box 暗号化。登録後は値を閲覧不可 |
| `false` | GitHub Actions Variables | 平文。値は GitHub UI で確認可能 |

`environments` に named environment 名を指定すると、その Environment スコープに登録。省略時（空）はリポジトリレベルに登録。

> **注意**: GitHub の named environment は事前に作成が必要です。存在しない environment 名を指定するとエラーになります。

#### Cloudflare Workers における各フィールドの意味

| `secret` | 登録先 | 説明 |
|---|---|---|
| `true` | Worker Secret | secrets API 経由で登録。登録後は値を閲覧不可 |
| `false` | — | **警告を出してスキップ**（平文 vars は wrangler 設定の `[vars]` が管理するため） |

`environments` は環境ごとの Worker スクリプト名に解決されます（デフォルトは `<スクリプト名>-<環境名>`）。
対応規則と上書き方法は[Cloudflare Workers へ同期](#5-cloudflare-workers-へ同期)を参照してください。

### 定義に無い変数の削除（prune）

`env-sync.yaml` のトップレベルに `prune: true` を書く（または `--prune` フラグを付ける）と、リモートに存在するが **`variables` に宣言されていない** 変数を同期時に削除します。

```yaml
prune: true
prune_exclude:
  - BLOB_*      # 例: Vercel Blob Store が発行するトークンを保持
  - SENTRY_DSN

variables:
  DATABASE_URL: { secret: true }
```

安全のための動作:

- 削除対象は送信前に一覧表示され、削除がある場合は必ず `y/N` 確認が入ります（`--yes` でスキップ、`--dry-run` は表示のみ）。
- `variables` に宣言されたキーは、`.env` に値が無くても削除されません。
- `prune_exclude` の glob パターンに一致するキーは保持されます（大文字小文字非区別）。
- **Vercel**: システム変数と、インテグレーションが作成した変数（Blob Store・Marketplace 連携などで `configurationId` を持つもの）は自動で除外されます。
- **GitHub**: Actions Secrets / Variables を、repo レベルと定義ファイルに現れる named environment のスコープで削除します。
- **GCP**: `managed-by=env-sync` ラベル付き Secret のみ削除対象です。このラベルは env-sync が同期時に自動付与するため、env-sync 以外が作成した Secret には触れません。
- **Cloudflare**: 定義ファイルが対象とするスクリプトの Worker Secrets のみ削除対象です。その実行に現れない Worker のシークレットには触れません。

## オプション / 環境変数

| 項目 | 必須 | 説明 |
|------|------|------|
| `--provider <name>` | – | 同期先（デフォルト `vercel`）。現在 `vercel` / `github` / `gcp` / `cloudflare` が利用可 |
| `--vercel-project <name>` | – | 同期対象を config の `vercel.projects[].name` 1 件に絞る。未指定なら定義済み全プロジェクト |
| `--github-repo <name>` | – | 同期対象を config の `github.repos[].name` 1 件に絞る。未指定なら定義済み全リポジトリ |
| `--cloudflare-script <name>` | – | 同期対象を config の `cloudflare.scripts[].name` 1 件に絞る。未指定なら定義済み全 Worker |
| `--env <file>` | – | 値を読む env ファイル（デフォルト `.env`） |
| `--def <file>` | – | 定義 YAML（デフォルト `env-sync.yaml`） |
| `--dry-run` | – | 送信せず新規/更新の区別を含む登録予定一覧を表示 |
| `--yes` / `-y` | – | 更新(上書き)がある場合の確認をスキップ |
| `--prune` | – | 定義ファイルに無いリモートの変数を削除（定義ファイルの `prune: true` でも有効化可） |
| `--environments <list>` | – | 書き込み先環境をカンマ区切りで指定（例 `staging,preview`）。各変数の `environments` との積集合が空の変数はスキップ。`--prune` と併用すると削除スコープも指定環境に限定される |
| `--force` | – | `init` 時に既存の def ファイルを上書きする |
| `VERCEL_TOKEN` | ◯(Vercel) | Vercel アクセストークン（dry-run 時は不要） |
| `VERCEL_PROJECT_ID` | △(Vercel) | プロジェクト ID。未指定なら config ファイルまたは `.vercel/project.json` から自動取得 |
| `VERCEL_TEAM_ID` | –(Vercel) | チーム(Org) ID。未指定なら config ファイルまたは `.vercel/project.json` の `orgId` |
| `GITHUB_TOKEN` | ◯(GitHub) | GitHub アクセストークン（dry-run 時は不要） |
| `GITHUB_REPO` | –(GitHub) | `owner/repo` 形式。未指定なら config ファイルまたは `git remote origin` から自動取得 |
| `CLOUDFLARE_API_TOKEN` | ◯(Cloudflare) | Workers Scripts:Edit 権限を持つ API トークン（dry-run 時は不要） |
| `CLOUDFLARE_ACCOUNT_ID` | ◯(Cloudflare) | 対象アカウント ID。未指定なら config ファイルの `cloudflare.account_id` |
| `CLOUDFLARE_SCRIPT_NAME` | –(Cloudflare) | Worker スクリプト名。未指定なら config ファイルまたは wrangler 設定の `name` |
| `--lang <code>` | – | 表示言語（`en` / `ja`）。デフォルト `en` |
| `ENV_SYNC_LANG` | – | 表示言語コード（`en` / `ja`）。`--lang` より低優先 |

## 表示言語の設定

CLI メッセージを英語（デフォルト）または日本語で表示できます。

### 優先順位（高い順）

1. `--lang <code>` フラグ
2. `ENV_SYNC_LANG` 環境変数
3. config ファイルの `language:` フィールド（`.env-sync.config.yaml` / `~/.config/env-sync/config.yaml`）
4. デフォルト: `en`

### config ファイルでの設定例

```yaml
# .env-sync.config.yaml
language: ja
vercel:
  token: ${VERCEL_TOKEN}
  project_id: <プロジェクト ID>
```

### 対応言語

| コード | 言語 |
|--------|------|
| `en` | 英語（デフォルト） |
| `ja` | 日本語 |

未対応のコードを指定した場合は `en` にフォールバックします。
