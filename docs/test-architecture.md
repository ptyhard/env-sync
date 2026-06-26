# テストアーキテクチャ規約

> 最終更新: 2026-06-26

## テスト戦略

`env-sync` は外部 API（Vercel / GitHub）への副作用を伴う CLI のため、テストは主に **リグレッション防止** と **API リクエスト仕様の固定**（正しいパス・メソッド・ヘッダ・ボディを送っているかの検証）を目的とする。実際の Vercel/GitHub には接続せず、`httptest.Server` で API を再現して検証する。純粋関数（定義解決・パース・展開）は入出力を直接検証する。

## テストスコープ

| コード種別 | テスト種別 | 備考 |
|-----------|-----------|------|
| 純粋関数（`parseDotenv` / `parseFlags` / `resolveEntries` / `deduplicateEnvironments` / `expandGitHubTasks` / `entriesToVercelItems` / `parseGitHubRemoteURL` / `buildInitYAML`） | ユニットテスト | 入出力を直接検証 |
| Provider registry（`registerProvider` / `lookupProvider` / `registeredProviderNames`） | ユニットテスト | 登録・参照・順序を検証（`provider_test.go`） |
| GitHub API クライアント（`githubPublicKey` / `githubPutSecret` / `githubVariableExists` 等） | 統合テスト（httptest） | パス・メソッド・ヘッダ・ボディを検証 |
| 暗号化（`encryptSecret`） | ユニットテスト | NaCl box の往復・出力形式を検証 |
| `vercelProvider.Sync` / `githubProvider.Sync` のフロー | 統合テスト（httptest） | Entry → provider 表現の翻訳と送信挙動を検証 |
| `main()` / `run()` のプロセス終了・I/O | スキップ可 | `os.Exit` を含むため、ロジックは下位関数側でカバー |

## フレームワーク

| 種別 | ツール | 実行コマンド |
|------|-------|------------|
| ユニット / 統合 | 標準 `testing` + `net/http/httptest` | `go test ./...` |
| レース検出（CI） | 標準 `testing -race` | `go test -race ./...` |
| 静的解析 | `go vet` | `go vet ./...` |
| フォーマット | `gofmt` | `gofmt -l .`（CI で未フォーマットを検出） |
| ビルド確認 | `go build` | `go build ./...` |

追加のテストフレームワーク・アサーションライブラリは導入しない（標準 `testing` のみ）。

## モック方針

- **外部 HTTP**: モックライブラリは使わず、`httptest.NewServer` で実 HTTP サーバを立てる。GitHub は差し替え可能な `var githubAPIBase` をテストサーバの URL に向ける（`withGitHubAPIBase` ヘルパーで `t.Cleanup` により復元）。
- **DB**: 無し（不要）。
- **時刻・乱数**: 暗号化テストでは `crypto/rand` をそのまま使い、鍵は `box.GenerateKey` で都度生成する（固定しない）。
- **環境変数**: `os.Getenv` 依存の箇所は `t.Setenv` で設定する。

```go
// github_integration_test.go — テスト中だけ API ベース URL を差し替える
func withGitHubAPIBase(t *testing.T, base string) {
    t.Helper()
    orig := githubAPIBase
    githubAPIBase = base
    t.Cleanup(func() { githubAPIBase = orig })
}
```

## テストデータ戦略

- factory / fixture ファイルは使わず、テスト関数内でリテラル（`Entry` スライス、`map[string]string`、定義 YAML 文字列）を直接組み立てる。
- 一時ファイルが必要な場合は `t.TempDir()` を使い、後始末は自動に任せる。
- 公開鍵など暗号化に必要なデータはヘルパー（`genTestPublicKey`）で生成する。

## ファイル配置

- 実装と**同階層（同ディレクトリ）**に `<対象>_test.go` を置く。必要に応じて `*_test` 形式の別パッケージ（外部テストパッケージ）を使う。各テストは対応する実装と同ディレクトリに配置する：
  - `internal/config/` … `config_test.go` / `dotenv_test.go` / `helpers_test.go` / `init_test.go`
  - `internal/sync/` … `entry_test.go`
  - `internal/provider/` … `provider_test.go`
  - `internal/provider/vercel/` … `vercel_test.go`
  - `internal/provider/github/` … `github_test.go` / `github_integration_test.go`
  - `cmd/env-sync/` … `main_test.go`（ビルドしたバイナリの統合テスト）
- GitHub 系は純粋ヘルパーの `github_test.go` と、httptest を使う `github_integration_test.go` に分ける。
- テスト関数は `TestXxx_条件`（例: `TestParseFlags_DryRun`, `TestGitHubPublicKey_EnvironmentScope`）。
- アサーションは `if got != want { t.Errorf("... got %q, want %q", got, want) }` の形。メッセージは**日本語**。

## CI

- `.github/workflows/ci.yml` が `push`（main ブランチ）と `pull_request` で起動し、**gofmt チェック → `go vet ./...` → `go build ./...` → `go test -race ./...`** を順に実行する。いずれか失敗で CI は赤になる。
- `.github/workflows/release.yml` は `v*` タグ push 時の GoReleaser 実行（テストは行わない）。

## 注意事項

- 新しい provider を足したら、registry への登録（`provider_test.go`）と、その provider の `Sync` / Entry 翻訳関数（純粋関数部分）のテストを追加する。
- テストでも秘匿値を実値として出力しない（ログ・エラーメッセージに値を含めない）。
- ローカルでも push 前に `gofmt -l .` が空であることと `go test -race ./...` の成功を確認すると CI 落ちを防げる。
