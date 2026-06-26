# アーキテクチャ規約

> 最終更新: 2026-06-26

`env-sync` は、定義ファイル `env-sync.yaml` で宣言した環境変数を **Vercel** または **GitHub Actions** へ一括同期する Go 製の単一バイナリ CLI。値は定義ファイルには書かず `.env(.production)` から読み込む。

## 技術スタック

| カテゴリ | 採用技術 | バージョン |
|---------|---------|----------|
| 言語 | Go | 1.25.4（`go.mod`） |
| 配布 | GoReleaser v2 + Homebrew Cask | — |
| YAML パース | `gopkg.in/yaml.v3` | 3.0.1 |
| 暗号化（GitHub Secrets） | `golang.org/x/crypto/nacl/box` | crypto 0.53.0 |
| 端末判定 | `golang.org/x/term` | 0.44.0 |
| HTTP | 標準ライブラリ `net/http` | — |
| テスト | 標準 `testing` + `net/http/httptest` | — |
| CI | GitHub Actions（`.github/workflows/ci.yml`） | — |

外部 Web フレームワーク・ORM・DB は使用しない。HTTP クライアントは標準ライブラリのみ。

## ディレクトリ全体図

```
.
├── main.go            # エントリポイント。run() のオーケストレーションと printUsage
├── config.go          # options / definition / varConf / parseFlags、const apiBase
├── entry.go           # 共通ドメインモデル Entry と resolveEntries（provider 非依存）
├── provider.go        # Provider インターフェースと registry（自己登録の仕組み）
├── vercel.go          # vercelProvider（Vercel REST API への同期）
├── github.go          # githubProvider（GitHub Actions Secrets/Variables への同期）
├── dotenv.go          # .env パーサ（parseDotenv）
├── helpers.go         # fileExists / isTTY / sortedKeys 等の汎用ヘルパー
├── init.go            # init サブコマンド（.env から env-sync.yaml 雛形生成）
├── *_test.go          # 各ファイルに対応するテスト（同パッケージ）
├── env-sync.yaml      # secret / environments の定義ファイル（値は書かない）
├── go.mod / go.sum
├── .goreleaser.yaml
└── .github/workflows/
    ├── ci.yml         # push(main) / PR で gofmt・vet・build・test
    └── release.yml    # v* タグ push で GoReleaser
```

**単一 `package main` のマルチファイル構成**。すべての型・関数は `package main` に属するが、責務ごとにファイルへ分割されている（タスク #9 / PR #10 のリファクタで `main.go` 集約から分割された）。

## アーキテクチャ方針

処理は `run()`（`main.go:59`）を起点とする手続き的パイプライン:

```
run()
 ├─ init サブコマンドなら runInit() へ（init.go）
 ├─ parseFlags()             … CLI フラグ → options（config.go）
 ├─ env / 定義ファイル読込    … parseDotenv（dotenv.go）+ yaml.Unmarshal → definition
 ├─ 整合性チェック            … 定義と env の差分を警告（provider 共通）
 ├─ resolveEntries()         … definition + envVars → []Entry（共通ドメインモデルへ変換）
 └─ lookupProvider(opts.provider).Sync(opts, entries)
```

### Provider 抽象（インターフェース + registry）

provider は `Provider` インターフェース（`provider.go`）で抽象化され、各実装は `init()` で自己登録する。`run()` は具象 provider を知らず、registry から引いて `Sync` を呼ぶだけ。

```go
// provider.go — 同期先を抽象化するインターフェース
type Provider interface {
    Name() string
    Sync(opts options, entries []Entry) error
}

// 名前 → ファクトリ関数の registry。各 provider の init() から登録する。
var providerRegistry = map[string]func() Provider{}

func registerProvider(name string, factory func() Provider) { ... }
func lookupProvider(name string) (Provider, bool) { ... }
```

```go
// vercel.go — provider は自分のファイルの init() で自己登録する
func init() {
    registerProvider("vercel", func() Provider { return &vercelProvider{} })
}
```

**新しい同期先を追加する手順**: 新規ファイル（例 `cloudflare.go`）を作り、`Provider` を実装する struct と、`init()` での `registerProvider("名前", ...)` を書く。`run()` や `parseFlags` の分岐に手を入れる必要はない（`parseFlags` は registry に登録済みかで `--provider` を検証する）。

### 共通ドメインモデル Entry

provider 非依存の「登録する環境変数 1 件」は `Entry`（`entry.go`）で表現する。`resolveEntries` が定義 + env 値からこれを生成し、各 provider が自分の表現へ翻訳する。

```go
// entry.go — provider 非依存の共通ドメインモデル
type Entry struct {
    Key          string
    Value        string
    Secret       bool      // true=シークレット, false=平文
    Environments []string  // 登録先環境（空なら provider 側のデフォルト）
}
```

provider 側での翻訳:

- **Vercel**（`entriesToVercelItems`, `vercel.go:154`）: `Secret` → `type`（true=`sensitive` / false=`plain`）、`Environments` → `target`（空なら `[production, preview]`）。`production|preview|development` のみ許可。
- **GitHub**（`expandGitHubTasks`, `github.go:43`）: `Secret` → Secret(sealed box 暗号化) / Variable(平文) の振り分け、`Environments` → named environment スコープ（空なら repo レベル。各環境ごとに task を展開）。

## 設定ファイル（env-sync.yaml）の構造

定義は `definition` 構造体（`config.go:27`）にマッピングされる。**値は書かず、`secret` / `environments` の宣言のみ**。

```go
// config.go
type varConf struct {
    Secret       *bool    `yaml:"secret"`       // nil=未指定（defaults にフォールバック）
    Environments []string `yaml:"environments"`
}
```

- `secret`: `true`=シークレット（Vercel `sensitive` / GitHub Secret）、`false`=平文（Vercel `plain` / GitHub Variable）。**既定は `true`（安全側）**。`*bool` で「未指定」と「明示 false」を区別する。
- `environments`: 登録先環境の配列。Vercel は `production|preview|development`、GitHub は named environment 名。空なら provider 側デフォルト。
- `defaults` セクションで全変数の既定値を指定でき、各変数の `varConf` が優先される（`resolveEntries` のフォールバック解決）。
- `environments` は `deduplicateEnvironments` で空文字除去・重複排除される。

> 旧スキーマの `type` / `target` / `kind` は廃止され、`secret`（bool）+ `environments` に統一された。`--github-env` フラグも廃止され、GitHub の environment スコープは `environments` で指定する。

## エラーハンドリング

- 致命的エラーは `die(format, ...)`（`fmt.Errorf` のラッパー、`main.go:55`）で `error` を返し、`main()` が `os.Stderr` に `エラー: %s` を出して `os.Exit(1)`。
- 個別変数の送信失敗は各 provider の `Sync` 内で集計し、`✓` / `✗` を逐次表示、最後に「成功 N / 失敗 N」を出して失敗があれば `os.Exit(1)`。
- API のエラーボディは `parseErrorBody`（Vercel, `vercel.go`）/ `parseGitHubErrorBody`（`github.go`）で要約してメッセージに付与する。
- フラグのパースエラーは `parseFlags` 内で直接 `os.Stderr` + `os.Exit(1)`（`error` を返さず即終了）。

## CLI フラグ

`parseFlags`（`config.go:37`）で手書きパース。`--flag value` と `--flag=value` の両形式、一部は `-flag` 短縮も受ける。

| フラグ | 既定 | 説明 |
|--------|------|------|
| `--provider <name>` | `vercel` | 同期先（registry に登録された provider 名のみ。未登録はエラー） |
| `--env <file>` | `.env` | 値を読む env ファイル |
| `--def <file>` | `env-sync.yaml` | 定義 YAML |
| `--dry-run` | false | 送信せず対象（key / secret / environments）のみ表示（値は出さない） |
| `--yes` / `-y` | false | 確認スキップ。非対話環境で未指定なら中止 |
| `--version` / `--help` | — | バージョン表示 / usage |

サブコマンド `init`（`init.go`）は `.env` から `env-sync.yaml` の雛形を生成する。

## 環境変数

| 変数 | provider | 用途 |
|------|----------|------|
| `VERCEL_TOKEN` | Vercel | アクセストークン（dry-run 時は不要） |
| `VERCEL_PROJECT_ID` | Vercel | 未指定なら `.vercel/project.json` から取得 |
| `VERCEL_TEAM_ID` | Vercel | 未指定なら `.vercel/project.json` の `orgId` |
| `GITHUB_TOKEN` | GitHub | アクセストークン（dry-run 時は不要） |
| `GITHUB_REPO` | GitHub | `owner/repo`。未指定なら `git remote origin` から自動解決 |

トークン等の秘匿値はすべて `os.Getenv` で取得し、コードやログには出さない。GitHub Secrets は送信前に NaCl box で sealed box 暗号化する（`encryptSecret`, `github.go:257`）。

## テスト・CI

- **配置**: 実装と同階層・同パッケージ（`*_test.go`、`package main`）。ファイルごとに対応するテスト（`config_test.go` / `entry_test.go` / `provider_test.go` / `vercel_test.go` / `github_test.go` / `github_integration_test.go` 等）。
- **実行**: `go test ./...`（CI は `-race` 付き）。静的解析 `go vet ./...`、フォーマット `gofmt`。
- **CI**（`.github/workflows/ci.yml`）: `push`（main）と `pull_request` で **gofmt チェック → `go vet ./...` → `go build ./...` → `go test -race ./...`** を実行。
- API 統合テストは `httptest.NewServer` を立て、`githubAPIBase` を `t.Cleanup` で差し替えるパターン（`withGitHubAPIBase`）。
- 詳細は `docs/test-architecture.md`。

## コーディング規約

- コメント・ユーザー向け出力・テストの assertion メッセージはすべて**日本語**。
- 関数・型には用途を 1 行で説明する doc コメントを付ける。
- 命名は Go 標準（エクスポート型は PascalCase、内部は camelCase）。`gofmt` 準拠（CI で強制）。
- ファイルは責務単位で分割し、provider は 1 ファイル 1 provider（`init()` で自己登録）。

## プロジェクト固有の注意事項

- **値は絶対に `env-sync.yaml` に書かない**（git にコミットされるため）。定義は宣言のみ、値は `.env` から。
- 定義に無いキーが `.env` にあってもスキップし警告する（ホワイトリスト方式、`main.go:96-105`）。
- `--dry-run` でも値は一切出力しない。
- 配布は Homebrew **Cask**（GoReleaser v2.16 以降 formula 廃止のため）。tap への push には自リポジトリ外書き込み用の `HOMEBREW_TAP_TOKEN`（Fine-grained PAT）が必要。

### 今後の方向（任意）

現状は単一 `package main` のままファイル分割している。さらに境界を強くするなら、[golang-standards/project-layout](https://github.com/golang-standards/project-layout) を参考に `cmd/env-sync`（`package main` はここだけ）+ `internal/{config,sync,provider/...}` への分割も選択肢（小規模 CLI のため必須ではない。`pkg/` は公開予定が無いので作らない）。採用する場合はロジックを `package main` に残さず役割名パッケージへ移し、`go install` ターゲットが `cmd/env-sync` に移る点と README/リリース手順の更新に注意する。
