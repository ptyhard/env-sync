# env-sync

定義ファイル `env-sync.yaml` で宣言した環境変数を **Vercel** または **GitHub Actions** へ一括登録（同期）する Go 製 CLI。
1 つの定義ファイルから、変数ごとに同期先を選んで Vercel / GitHub Actions の両方へまとめて反映できる。

- **Vercel**: REST API (`POST /v10/projects/{id}/env?upsert=true`) を使うため、再実行すると既存の変数は **更新（upsert）** される。
- **GitHub Actions**: Secrets（sealed box 暗号化）と Variables（平文）の両方に対応する。

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
# 1. 既存の .env から定義ファイルの雛形を生成
env-sync init

# 2. 生成された env-sync.yaml の secret / environments / provider を見直す

# 3. 送信せずに登録予定を確認（dry-run）
VERCEL_TOKEN=xxxxx env-sync --env .env.production --dry-run

# 4. 本番投入（更新がある場合のみ y/N 確認）
VERCEL_TOKEN=xxxxx env-sync --env .env.production
```

## 1. init で雛形を生成

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

## 2. Vercel へ同期

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

## 3. GitHub Actions へ同期

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

## 4. Vercel と GitHub Actions を混在させる

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

## 5. config ファイルで認証情報・ID を管理する

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
```

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
| `secret` | `bool` | `true`（デフォルト）: シークレット登録 / `false`: 平文登録 |
| `environments` | `[]string` | 登録先環境の配列。省略すると `defaults.environments` を継承 |
| `provider` | `string` または `[]string` | 同期先プロバイダー。省略すると `defaults.provider` → CLI `--provider` フラグを使用 |

#### Vercel における各フィールドの意味

| `secret` | Vercel type | 説明 |
|---|---|---|
| `true` | `sensitive` | 保存後は値を読めないシークレット向け |
| `false` | `plain` | 暗号化なしの平文 |

`environments` には `production` / `preview` / `development` を指定。省略時（空）は `production` と `preview` をデフォルト適用。

#### GitHub Actions における各フィールドの意味

| `secret` | 登録先 | 説明 |
|---|---|---|
| `true` | GitHub Actions Secrets | sealed box 暗号化。登録後は値を閲覧不可 |
| `false` | GitHub Actions Variables | 平文。値は GitHub UI で確認可能 |

`environments` に named environment 名を指定すると、その Environment スコープに登録。省略時（空）はリポジトリレベルに登録。

> **注意**: GitHub の named environment は事前に作成が必要です。存在しない environment 名を指定するとエラーになります。

## オプション / 環境変数

| 項目 | 必須 | 説明 |
|------|------|------|
| `--provider <name>` | – | 同期先（デフォルト `vercel`）。現在 `vercel` / `github` が利用可 |
| `--env <file>` | – | 値を読む env ファイル（デフォルト `.env`） |
| `--def <file>` | – | 定義 YAML（デフォルト `env-sync.yaml`） |
| `--dry-run` | – | 送信せず新規/更新の区別を含む登録予定一覧を表示 |
| `--yes` / `-y` | – | 更新(上書き)がある場合の確認をスキップ |
| `--force` | – | `init` 時に既存の def ファイルを上書きする |
| `VERCEL_TOKEN` | ◯(Vercel) | Vercel アクセストークン（dry-run 時は不要） |
| `VERCEL_PROJECT_ID` | △(Vercel) | プロジェクト ID。未指定なら config ファイルまたは `.vercel/project.json` から自動取得 |
| `VERCEL_TEAM_ID` | –(Vercel) | チーム(Org) ID。未指定なら config ファイルまたは `.vercel/project.json` の `orgId` |
| `GITHUB_TOKEN` | ◯(GitHub) | GitHub アクセストークン（dry-run 時は不要） |
| `GITHUB_REPO` | –(GitHub) | `owner/repo` 形式。未指定なら config ファイルまたは `git remote origin` から自動取得 |
