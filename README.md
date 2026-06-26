# vercel-env-sync

定義ファイル `vercel-env.yaml` で宣言した環境変数を Vercel へ **一括登録（同期）** する Go 製 CLI。
Vercel REST API (`POST /v10/projects/{id}/env?upsert=true`) を使うため、再実行すると既存の変数は **更新（upsert）** される。

[ptyhard/arg-next の `scripts/vercel-env-push.mjs`](../arg-next/scripts/vercel-env-push.mjs) と同じ仕様を Go で実装したもの。

## 仕組み

- **type / target は `vercel-env.yaml` で明示的に宣言**する（キー名のヒューリスティックに頼らない）。
- **値は `vercel-env.yaml` には書かない**（git にコミットされるため）。値は `.env(.production)` から取得する。
- 定義に無いキーは登録されない（`.env` にあっても警告のうえスキップ）。
- 定義にあるが `.env` に値が無いキーも警告のうえスキップ。

## インストール

### Homebrew（macOS / Linux）

```bash
brew install ptyhard/tap/vercel-env-sync
```

> GoReleaser v2.16 以降は formula が廃止されたため、配布は Homebrew **Cask** で行います。`brew install ptyhard/tap/vercel-env-sync` は tap リポジトリ [ptyhard/homebrew-tap](https://github.com/ptyhard/homebrew-tap) の cask を解決します。明示するなら `brew install --cask ptyhard/tap/vercel-env-sync` でも構いません。

### go install

```bash
go install github.com/ptyhard/vercel-env-sync@latest
```

### バイナリを直接ダウンロード

[GitHub Releases](https://github.com/ptyhard/vercel-env-sync/releases) から最新バイナリをダウンロードしてください。
darwin/amd64、darwin/arm64、linux/amd64、linux/arm64 向けアーカイブが提供されます。

### ソースからビルド

```bash
go build -o vercel-env-sync .
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
2. [ptyhard/vercel-env-sync の Secrets](https://github.com/ptyhard/vercel-env-sync/settings/secrets/actions) に `HOMEBREW_TAP_TOKEN` として登録する。

> **注意**: tap リポジトリ `ptyhard/homebrew-tap` が未作成の場合は先に作成してください（`gh repo create ptyhard/homebrew-tap --public`）。

## 定義ファイル `vercel-env.yaml`

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

既存の `.env` を読み込み、値を含まない `vercel-env.yaml` の雛形を自動生成します。

```bash
# 基本（.env から vercel-env.yaml を生成）
./vercel-env-sync init

# 別ファイルを指定
./vercel-env-sync init --env .env.production

# 出力先を指定
./vercel-env-sync init --env .env.production --def vercel-env.production.yaml

# 既存ファイルを上書き（--force なしでは上書きを拒否してエラー）
./vercel-env-sync init --env .env.production --force
```

- `NEXT_PUBLIC_` プレフィックスのキーは `encrypted`、それ以外は `sensitive` を初期値として設定します
- これはあくまで**雛形**です。type は投入前に必ず見直してください
- **値は一切書き込まれません**（`.env` の値が yaml に混入することはありません）
- 既存の `vercel-env.yaml` がある場合、`--force` なしでは上書きを拒否してエラーで終了します

## 使い方

```bash
# dry-run（送信せず key / type / target だけ確認。値は表示されない）
VERCEL_PROJECT_ID=dummy ./vercel-env-sync --env .env.production --dry-run

# 本番投入（登録対象を一覧表示し、y/N の確認のうえ送信する）
VERCEL_TOKEN=xxxxx ./vercel-env-sync --env .env.production

# 確認をスキップ（CI など）
VERCEL_TOKEN=xxxxx ./vercel-env-sync --env .env.production --yes
```

送信前に登録対象（key / type / target）を一覧表示し、既存変数は upsert で上書きされるため `y/N` の確認を取る。`--yes`（`-y`）で確認をスキップできる。非対話環境で `--yes` を付けないと安全のため中止する。

## オプション / 環境変数

| 項目 | 必須 | 説明 |
|------|------|------|
| `--env <file>` | – | 値を読む env ファイル（デフォルト `.env`） |
| `--def <file>` | – | type/target 定義 YAML（デフォルト `vercel-env.yaml`） |
| `--dry-run` | – | 送信せず登録予定のみ表示 |
| `--yes` / `-y` | – | 送信前の確認をスキップ |
| `--force` | – | `init` 時に既存の def ファイルを上書きする |
| `VERCEL_TOKEN` | ◯ | Vercel アクセストークン（dry-run 時は不要） |
| `VERCEL_PROJECT_ID` | △ | プロジェクト ID。未指定なら `.vercel/project.json` から自動取得 |
| `VERCEL_TEAM_ID` | – | チーム(Org) ID。未指定なら `.vercel/project.json` の `orgId` |
