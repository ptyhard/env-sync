# env-sync

定義ファイル `env-sync.yaml` で宣言した環境変数を **Vercel** または **GitHub Actions** へ一括登録（同期）する Go 製 CLI。
Vercel モードでは Vercel REST API (`POST /v10/projects/{id}/env?upsert=true`) を使うため、再実行すると既存の変数は **更新（upsert）** される。
GitHub Actions モードでは Secrets（sealed box 暗号化）と Variables（平文）の両方に対応する。

arg-next の env 同期スクリプトと同じ仕様を Go で実装したもの。

## 仕組み

- **type / target は `env-sync.yaml` で明示的に宣言**する（キー名のヒューリスティックに頼らない）。
- **値は `env-sync.yaml` には書かない**（git にコミットされるため）。値は `.env(.production)` から取得する。
- 定義に無いキーは登録されない（`.env` にあっても警告のうえスキップ）。
- 定義にあるが `.env` に値が無いキーも警告のうえスキップ。

## インストール

### Homebrew（macOS / Linux）

```bash
brew install ptyhard/tap/env-sync
```

> GoReleaser v2.16 以降は formula が廃止されたため、配布は Homebrew **Cask** で行います。`brew install ptyhard/tap/env-sync` は tap リポジトリ [ptyhard/homebrew-tap](https://github.com/ptyhard/homebrew-tap) の cask を解決します。明示するなら `brew install --cask ptyhard/tap/env-sync` でも構いません。

### go install

```bash
go install github.com/ptyhard/env-sync@latest
```

### バイナリを直接ダウンロード

[GitHub Releases](https://github.com/ptyhard/env-sync/releases) から最新バイナリをダウンロードしてください。
darwin/amd64、darwin/arm64、linux/amd64、linux/arm64 向けアーカイブが提供されます。

### ソースからビルド

```bash
go build -o env-sync .
```

## リリース（メンテナ向け）

`v*` タグを push すると [.github/workflows/release.yml](.github/workflows/release.yml) が GoReleaser を実行し、GitHub Release の作成と tap リポジトリ [ptyhard/homebrew-tap](https://github.com/ptyhard/homebrew-tap) への cask 生成・push を行います。

```bash
git tag v0.1.0
git push origin v0.1.0
```

### `HOMEBREW_TAP_TOKEN` の設定（初回のみ）

cask を別リポジトリ `ptyhard/homebrew-tap` へ push するため、`GITHUB_TOKEN`（自リポジトリのみ書き込み可）とは別に Personal Access Token が必要です。

1. [Fine-grained PAT](https://github.com/settings/personal-access-tokens/new) を発行する。Repository access は `ptyhard/homebrew-tap`、権限は **Contents: Read and write**。
2. [ptyhard/env-sync の Secrets](https://github.com/ptyhard/env-sync/settings/secrets/actions) に `HOMEBREW_TAP_TOKEN` として登録する。

> **注意**: tap リポジトリ `ptyhard/homebrew-tap` が未作成の場合は先に作成してください（`gh repo create ptyhard/homebrew-tap --public`）。

## 定義ファイル `env-sync.yaml`

```yaml
defaults:
  target: [production, preview]
  type: sensitive
variables:
  NEXT_PUBLIC_FIREBASE_API_KEY: { type: encrypted }
  DATABASE_URL: { type: sensitive, target: [production] }
```

| type | Vercel UI | 説明 |
|------|-----------|------|
| `encrypted` | Variables | ダッシュボードで値を再表示できる通常の変数 |
| `sensitive` | Sensitive | 保存後は値を読めないシークレット向け |
| `plain` | Plain | 暗号化なしの平文 |

`target` は `production` / `preview` / `development` の配列。各変数で省略すると `defaults` が使われる。

## 事前準備

```bash
# 1. プロジェクトをリンク（初回のみ。.vercel/project.json が生成される）
vercel link

# 2. アクセストークンを発行
#    https://vercel.com/account/tokens
```

## init で雛形を生成

既存の `.env` を読み込み、値を含まない `env-sync.yaml` の雛形を自動生成します。

```bash
# 基本（.env から env-sync.yaml を生成）
./env-sync init

# 別ファイルを指定
./env-sync init --env .env.production

# 出力先を指定
./env-sync init --env .env.production --def env-sync.production.yaml

# 既存ファイルを上書き（--force なしでは上書きを拒否してエラー）
./env-sync init --env .env.production --force
```

- `NEXT_PUBLIC_` プレフィックスのキーは `encrypted`、それ以外は `sensitive` を初期値として設定します
- これはあくまで**雛形**です。type は投入前に必ず見直してください
- **値は一切書き込まれません**（`.env` の値が yaml に混入することはありません）
- 既存の `env-sync.yaml` がある場合、`--force` なしでは上書きを拒否してエラーで終了します

## 使い方

```bash
# dry-run（送信せず key / type / target だけ確認。値は表示されない）
VERCEL_PROJECT_ID=dummy ./env-sync --env .env.production --dry-run

# 本番投入（登録対象を一覧表示し、y/N の確認のうえ送信する）
VERCEL_TOKEN=xxxxx ./env-sync --env .env.production

# 確認をスキップ（CI など）
VERCEL_TOKEN=xxxxx ./env-sync --env .env.production --yes
```

送信前に登録対象（key / type / target）を一覧表示し、既存変数は upsert で上書きされるため `y/N` の確認を取る。`--yes`（`-y`）で確認をスキップできる。非対話環境で `--yes` を付けないと安全のため中止する。

## オプション / 環境変数

| 項目 | 必須 | 説明 |
|------|------|------|
| `--provider vercel\|github` | – | 同期先（デフォルト `vercel`） |
| `--env <file>` | – | 値を読む env ファイル（デフォルト `.env`） |
| `--def <file>` | – | type/target 定義 YAML（デフォルト `env-sync.yaml`） |
| `--dry-run` | – | 送信せず登録予定のみ表示 |
| `--yes` / `-y` | – | 送信前の確認をスキップ |
| `--github-env <name>` | – | GitHub Actions の Environment スコープ（未指定はリポジトリレベル） |
| `--force` | – | `init` 時に既存の def ファイルを上書きする |
| `VERCEL_TOKEN` | ◯(Vercel) | Vercel アクセストークン（dry-run 時は不要） |
| `VERCEL_PROJECT_ID` | △(Vercel) | プロジェクト ID。未指定なら `.vercel/project.json` から自動取得 |
| `VERCEL_TEAM_ID` | –(Vercel) | チーム(Org) ID。未指定なら `.vercel/project.json` の `orgId` |
| `GITHUB_TOKEN` | ◯(GitHub) | GitHub アクセストークン（dry-run 時は不要） |
| `GITHUB_REPO` | –(GitHub) | `owner/repo` 形式。未指定なら `git remote origin` から自動取得 |

## GitHub Actions モード

`--provider github` を指定すると GitHub Actions の Secrets/Variables に同期します。

```bash
# dry-run（送信せず key / kind だけ確認。値は表示されない）
GITHUB_REPO=owner/repo ./env-sync --provider github --env .env.production --dry-run

# 本番投入（リポジトリレベル）
GITHUB_TOKEN=xxxxx GITHUB_REPO=owner/repo ./env-sync --provider github --env .env.production

# Environment スコープに登録
GITHUB_TOKEN=xxxxx ./env-sync --provider github --github-env production --env .env.production

# 確認をスキップ（CI など）
GITHUB_TOKEN=xxxxx ./env-sync --provider github --yes --env .env.production
```

### `kind` フィールド

`env-sync.yaml` の各変数に `kind` を追加することで登録先を制御します。

```yaml
defaults:
  kind: secret   # 未指定時のデフォルト（安全側）

variables:
  DATABASE_URL:
    kind: secret    # GitHub Actions Secrets として登録（sealed box 暗号化）
  PUBLIC_FLAG:
    kind: variable  # GitHub Actions Variables として登録（平文）
```

| kind | 登録先 | 暗号化 | 説明 |
|------|--------|--------|------|
| `secret` | GitHub Actions Secrets | sealed box (NaCl) | 登録後は値を閲覧不可 |
| `variable` | GitHub Actions Variables | なし（平文） | 値は GitHub UI で確認可能 |

### セキュリティ

- 秘密値（`kind: secret`）は **GitHub の公開鍵で sealed box 暗号化**してから PUT します。平文は送信しません。
- 登録対象の一覧表示・確認プロンプト・成否ログのいずれにも **値は出力されません**。`--dry-run` でも同様。
- 公開鍵の長さ（32 バイト）を検証し、不正な鍵では処理を中止します。
