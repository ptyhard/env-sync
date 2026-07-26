// Package cloudflare は Cloudflare Workers のシークレット同期を実装する provider。
//
// Workers の環境変数は「Secrets」と「平文 vars」の 2 種類あるが、本 provider は
// Secrets のみを同期する。平文 vars は wrangler 設定の [vars] セクションが管理しており、
// API で設定しても次の `wrangler deploy` で上書きされて消えるため（Secrets はデプロイを
// またいで保持される）、secret=false のエントリは警告のうえスキップする。
package cloudflare

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// apiBase は Cloudflare REST API のベース URL。テストで httptest.Server を
// 指す差し替えができるよう var にしている。
var apiBase = "https://api.cloudflare.com/client/v4"

// httpTimeout は Cloudflare API 呼び出しの HTTP タイムアウト。
const httpTimeout = 30 * time.Second

// syncOsExit は Sync の失敗終了に使う関数。テストで差し替えられるよう var にしている。
var syncOsExit = os.Exit

func init() {
	provider.RegisterProvider("cloudflare", func() provider.Provider { return &cloudflareProvider{} })
}

// cloudflareProvider は Cloudflare Workers への同期を担当する Provider 実装。
type cloudflareProvider struct{}

func (c *cloudflareProvider) Name() string { return "cloudflare" }

// cloudflareTask は「1 つの Worker スクリプトへ登録する 1 件」を表す。
// Workers の環境（wrangler の [env.staging]）は独立したスクリプトとしてデプロイされるため、
// 1 つの Entry が複数の環境を宣言している場合は環境ごとに task へ展開される。
type cloudflareTask struct {
	script string
	entry  provider.Entry
}

// classifiedTask は task に新規/更新の分類情報を付加した型。
type classifiedTask struct {
	task  cloudflareTask
	isNew bool // true=新規, false=更新
}

// pruneTarget は prune で削除する 1 件（スクリプト名 + シークレット名）を表す。
type pruneTarget struct {
	script string
	name   string
}

// resolveScriptName は環境名から Worker スクリプト名を解決する純粋関数。
// env が空ならベーススクリプト名をそのまま返す。
// mapping（config の cloudflare.environments）に環境名があればその値を優先し、
// 無ければ wrangler の慣習に合わせて "<base>-<env>" を返す
// （wrangler は [env.staging] を my-worker-staging としてデプロイする）。
func resolveScriptName(base, env string, mapping map[string]string) string {
	if env == "" {
		return base
	}
	if s, ok := mapping[env]; ok && s != "" {
		return s
	}
	return base + "-" + env
}

// expandCloudflareTasks は entries を (スクリプト, Entry) の組へ展開する純粋関数。
// Environments が空の Entry はベーススクリプトへの 1 件になる。
// 複数環境が同一スクリプト名に解決された場合は重複を排除する（同じ値の二重送信を防ぐ）。
func expandCloudflareTasks(entries []provider.Entry, base string, mapping map[string]string) []cloudflareTask {
	var tasks []cloudflareTask
	for _, e := range entries {
		envs := e.Environments
		if len(envs) == 0 {
			tasks = append(tasks, cloudflareTask{script: base, entry: e})
			continue
		}
		seen := make(map[string]bool, len(envs))
		for _, env := range envs {
			script := resolveScriptName(base, env, mapping)
			if seen[script] {
				continue
			}
			seen[script] = true
			tasks = append(tasks, cloudflareTask{script: script, entry: e})
		}
	}
	return tasks
}

// taskScripts は tasks に現れるスクリプト名を出現順で重複なく返す純粋関数。
func taskScripts(tasks []cloudflareTask) []string {
	var scripts []string
	seen := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		if seen[t.script] {
			continue
		}
		seen[t.script] = true
		scripts = append(scripts, t.script)
	}
	return scripts
}

// classifyTasks は tasks をスクリプトごとの既存シークレット名セットと照合して新規/更新に分類する純粋関数。
func classifyTasks(tasks []cloudflareTask, existing map[string]map[string]bool) []classifiedTask {
	result := make([]classifiedTask, len(tasks))
	for i, t := range tasks {
		result[i] = classifiedTask{task: t, isNew: !existing[t.script][t.entry.Key]}
	}
	return result
}

// countClassified は classified から新規件数・更新件数を返す。
// classified が nil（分類不可）のときは新規=total、更新=0 を返す。
func countClassified(classified []classifiedTask, total int) (newCount, updateCount int) {
	if classified == nil {
		return total, 0
	}
	for _, c := range classified {
		if c.isNew {
			newCount++
		} else {
			updateCount++
		}
	}
	return
}

// computePrune は既存シークレット名のうち定義ファイルに無いものを削除対象として返す純粋関数。
// keep は Options.PruneKeep が返す保持判定（定義済みキー + prune_exclude パターン）。
// scripts は走査順を決定的にするためのスクリプト名リスト（map の反復順に依存しない）。
func computePrune(scripts []string, existing map[string][]string, keep func(key string) bool) []pruneTarget {
	var targets []pruneTarget
	for _, script := range scripts {
		for _, name := range existing[script] {
			if keep(name) {
				continue
			}
			targets = append(targets, pruneTarget{script: script, name: name})
		}
	}
	return targets
}

// Sync は Cloudflare Workers への環境変数（シークレット）同期を行う。
// Entry.Secret == false のエントリはスキップする（平文 vars は wrangler 設定が管理するため）。
func (c *cloudflareProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}
	if !opts.DryRun && appCfg.ResolveCloudflareAPIToken() == "" && len(appCfg.Cloudflare.Scripts) == 0 {
		return fmt.Errorf("%s", i18n.T(i18n.MsgCloudflareTokenMissing))
	}

	targets, err := appCfg.ResolveCloudflareTargets(opts.CloudflareScript)
	if err != nil {
		return err
	}

	// ---- secret=false のエントリを除外（平文 vars は同期しない） ----
	var secretEntries []provider.Entry
	skippedPlain := 0
	for _, e := range entries {
		if !e.Secret {
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgCloudflareSkipNotSecret, e.Key))
			skippedPlain++
			continue
		}
		secretEntries = append(secretEntries, e)
	}
	if skippedPlain > 0 {
		fmt.Fprint(os.Stderr, i18n.T(i18n.MsgCloudflareVarsNote))
	}

	// ---- 単一ターゲット時は wrangler 設定ファイルから script 名をフォールバック解決 ----
	if err := applyWranglerFallback(targets); err != nil {
		return err
	}

	client := &http.Client{Timeout: httpTimeout}
	pruneKeep := opts.PruneKeep()

	// resolvedTarget はターゲットごとの解決結果を保持し、確認・送信フェーズで再利用する。
	type resolvedTarget struct {
		label      string
		tasks      []cloudflareTask
		classified []classifiedTask
		prune      []pruneTarget
		skipped    bool // トークン未設定により送信をスキップするターゲット
	}
	resolved := make([]resolvedTarget, 0, len(targets))

	for _, tgt := range targets {
		label := tgt.Script
		if tgt.Name != "" {
			label = tgt.Name + " (" + tgt.Script + ")"
		}

		// トークン未設定チェック（per-target）
		// 単一ターゲット時は即エラー返却。複数ターゲット時は失敗として記録して残りを継続する。
		if !opts.DryRun && tgt.APIToken == "" {
			if len(targets) == 1 {
				return fmt.Errorf("%s", i18n.T(i18n.MsgCloudflareTokenMissingScript, tgt.Name))
			}
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgCloudflareTokenSkipScript, tgt.Name))
			// 送信できなかった件数を失敗として集計するため、スキップするターゲットでも tasks を展開しておく。
			// これを省くと送信フェーズの totalNG に 0 が加算され、ターゲットを丸ごと同期できなかったのに
			// 他ターゲットが成功すれば exit 0 になってしまう。
			// この経路に来るのは複数ターゲット時のみで、その場合 cloudflare.scripts[] は script 必須のため
			// tgt.Script は非空になる（validateCloudflareScriptConfs で検証済み）。
			skippedTasks := expandCloudflareTasks(secretEntries, tgt.Script, tgt.Environments)
			resolved = append(resolved, resolvedTarget{label: label, tasks: skippedTasks, skipped: true})
			continue
		}
		// script / accountId は dry-run でも解決できていないと表示すべき対象が定まらないためエラーにする。
		if tgt.Script == "" {
			return fmt.Errorf("%s", i18n.T(i18n.MsgCloudflareScriptMissing))
		}
		if !opts.DryRun && tgt.AccountID == "" {
			return fmt.Errorf("%s", i18n.T(i18n.MsgCloudflareAccountIDMissing))
		}

		tasks := expandCloudflareTasks(secretEntries, tgt.Script, tgt.Environments)
		scripts := taskScripts(tasks)

		// ---- 既存シークレット一覧の取得（新規/更新分類と prune に使う） ----
		var classified []classifiedTask
		var pruneTargets []pruneTarget
		if tgt.APIToken != "" && tgt.AccountID != "" {
			existing, listErr := fetchSecretNames(client, tgt.APIToken, tgt.AccountID, scripts)
			if listErr == nil {
				existingSet := make(map[string]map[string]bool, len(existing))
				for script, names := range existing {
					set := make(map[string]bool, len(names))
					for _, n := range names {
						set[n] = true
					}
					existingSet[script] = set
				}
				classified = classifyTasks(tasks, existingSet)
				if opts.Prune {
					pruneTargets = computePrune(scripts, existing, pruneKeep)
				}
			} else {
				// API 失敗時は classified = nil のまま（確認をスキップしない安全側フォールバック）。
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgCloudflareExistingKeysFetchWarn, listErr))
				// 既存一覧が取れない場合は何を消してよいか判定できないため prune もスキップする。
				if opts.Prune {
					fmt.Fprint(os.Stderr, i18n.T(i18n.MsgPruneSkipWarn, listErr))
				}
			}
		}
		resolved = append(resolved, resolvedTarget{label: label, tasks: tasks, classified: classified, prune: pruneTargets})

		// ---- 登録対象の一覧表示 ----
		fmt.Print(i18n.T(i18n.MsgCloudflareTargetScript, label, opts.Env, opts.Def))
		newCount, updateCount := countClassified(classified, len(tasks))
		if classified != nil {
			fmt.Print(i18n.T(i18n.MsgEntriesClassified, len(tasks), newCount, updateCount))
		} else {
			fmt.Print(i18n.T(i18n.MsgEntriesCount, len(tasks)))
		}
		for i, t := range tasks {
			if classified != nil {
				marker, statusLabel := "⟳", i18n.T(i18n.MsgLabelUpdate)
				if classified[i].isNew {
					marker, statusLabel = "+", i18n.T(i18n.MsgLabelNew)
				}
				fmt.Printf("  %s %-30s script=%s [%s]\n", marker, t.entry.Key, t.script, statusLabel)
			} else {
				fmt.Printf("  %-30s script=%s\n", t.entry.Key, t.script)
			}
		}
		// prune 削除対象の一覧表示
		if len(pruneTargets) > 0 {
			fmt.Print(i18n.T(i18n.MsgPruneEntries, len(pruneTargets)))
			for _, p := range pruneTargets {
				fmt.Printf("  - %-30s script=%s [%s]\n", p.name, p.script, i18n.T(i18n.MsgLabelDelete))
			}
		}
		fmt.Println()
	}

	totalTasks, totalPrune := 0, 0
	for _, r := range resolved {
		totalTasks += len(r.tasks)
		totalPrune += len(r.prune)
	}
	if totalTasks == 0 && totalPrune == 0 {
		fmt.Println(i18n.T(i18n.MsgNoEntries))
		return nil
	}
	if opts.DryRun {
		fmt.Println(i18n.T(i18n.MsgDryRun))
		return nil
	}

	// ---- 確認（更新がある場合、または分類不可の場合）----
	// 複数ターゲット時は常に確認（安全側）。単一ターゲット時は更新有無で判定。
	activeCount := 0
	for _, r := range resolved {
		if !r.skipped {
			activeCount++
		}
	}
	needsConfirm := false
	if activeCount > 1 {
		needsConfirm = true
	} else if activeCount == 1 {
		for _, r := range resolved {
			if !r.skipped {
				_, updateCount := countClassified(r.classified, len(r.tasks))
				needsConfirm = r.classified == nil || updateCount > 0
				break
			}
		}
	}
	// 削除は破壊的操作のため、prune 対象がある場合は必ず確認する
	if totalPrune > 0 {
		needsConfirm = true
		fmt.Print(i18n.T(i18n.MsgPruneConfirmNote, totalPrune))
	}
	if needsConfirm && !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("%s", i18n.T(i18n.MsgNonInteractiveErr))
		}
		if activeCount > 1 {
			fmt.Print(i18n.T(i18n.MsgCloudflareConfirmMulti, activeCount))
		} else {
			fmt.Print(i18n.T(i18n.MsgCloudflareConfirmSingle))
		}
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println(i18n.T(i18n.MsgAborted))
			return nil
		}
	}

	// ---- 各ターゲットへ送信 ----
	totalOK, totalNG := 0, 0
	for i, tgt := range targets {
		r := resolved[i]
		if r.skipped {
			// 一覧表示フェーズで警告済み。失敗件数として集計する。
			totalNG += len(r.tasks)
			continue
		}
		if activeCount > 1 {
			fmt.Print(i18n.T(i18n.MsgCloudflareScriptSeparator, r.label))
		}
		ok, ng := syncOneTarget(client, tgt.APIToken, tgt.AccountID, r.tasks)
		totalOK += ok
		totalNG += ng
		// prune 削除（登録完了後に実行）
		if len(r.prune) > 0 {
			dok, dng := deleteSecrets(client, tgt.APIToken, tgt.AccountID, r.prune)
			totalOK += dok
			totalNG += dng
		}
	}

	if activeCount > 1 {
		fmt.Print(i18n.T(i18n.MsgTotalCompleted, totalOK, totalNG))
	} else {
		fmt.Print(i18n.T(i18n.MsgCompleted, totalOK, totalNG))
	}
	if totalNG > 0 {
		syncOsExit(1)
	}
	return nil
}

// secretItem は Cloudflare へ送信する 1 件のシークレット。
type secretItem struct {
	Name string `json:"name"`
	Text string `json:"text"`
	Type string `json:"type"`
}

// syncOneTarget は tasks を Cloudflare へ送信し、成功数・失敗数を返す。os.Exit は呼ばない。
func syncOneTarget(client *http.Client, token, accountID string, tasks []cloudflareTask) (ok, ng int) {
	for _, t := range tasks {
		apiURL := secretsURL(accountID, t.script)
		body, _ := json.Marshal(secretItem{Name: t.entry.Key, Text: t.entry.Value, Type: "secret_text"})
		req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgCloudflareRequestCreateFailOut, t.entry.Key, err))
			ng++
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgCloudflareSendFailOut, t.entry.Key, err))
			ng++
			continue
		}
		if msg := checkResponse(res); msg == "" {
			fmt.Printf("✓ %s (script: %s)\n", t.entry.Key, t.script)
			ok++
		} else {
			fmt.Printf("✗ %s (script: %s) -> %s\n", t.entry.Key, t.script, msg)
			ng++
		}
		res.Body.Close()
	}
	return ok, ng
}

// deleteSecrets は prune 対象のシークレットを 1 件ずつ削除し、成功数・失敗数を返す。os.Exit は呼ばない。
func deleteSecrets(client *http.Client, token, accountID string, targets []pruneTarget) (ok, ng int) {
	for _, t := range targets {
		apiURL := secretsURL(accountID, t.script) + "/" + url.PathEscape(t.name)
		req, err := http.NewRequest(http.MethodDelete, apiURL, nil)
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgCloudflareRequestCreateFailOut, t.name, err))
			ng++
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)

		res, err := client.Do(req)
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgCloudflareSendFailOut, t.name, err))
			ng++
			continue
		}
		if msg := checkResponse(res); msg == "" {
			fmt.Printf("✓ %s (script: %s) [%s]\n", t.name, t.script, i18n.T(i18n.MsgLabelDelete))
			ok++
		} else {
			fmt.Printf("✗ %s (script: %s) -> %s\n", t.name, t.script, msg)
			ng++
		}
		res.Body.Close()
	}
	return ok, ng
}

// fetchSecretNames は各スクリプトに登録済みのシークレット名一覧を返す。
// 1 つでも取得に失敗した場合はエラーを返す（部分的な結果で prune 判定をしないため）。
func fetchSecretNames(client *http.Client, token, accountID string, scripts []string) (map[string][]string, error) {
	result := make(map[string][]string, len(scripts))
	for _, script := range scripts {
		names, err := fetchSecretNamesForScript(client, token, accountID, script)
		if err != nil {
			return nil, err
		}
		result[script] = names
	}
	return result, nil
}

// fetchSecretNamesForScript は 1 つの Worker スクリプトのシークレット名一覧を返す。
// このエンドポイントはページングを持たず、全件が result 配列で返る。
func fetchSecretNamesForScript(client *http.Client, token, accountID, script string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, secretsURL(accountID, script), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgCloudflareSecretsFetchFail), err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgCloudflareSecretsFetchFail), err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseErrorBody(data); detail != "" {
			msg += ": " + detail
		}
		return nil, fmt.Errorf("%s: %s", i18n.T(i18n.MsgCloudflareSecretsFetchFail), msg)
	}

	var body struct {
		Success bool `json:"success"`
		Result  []struct {
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgCloudflareSecretsParseFail), err)
	}
	// Cloudflare は HTTP 200 でも success:false を返すことがある。
	if !body.Success {
		msg := parseErrorBody(data)
		if msg == "" {
			msg = "success=false"
		}
		return nil, fmt.Errorf("%s: %s", i18n.T(i18n.MsgCloudflareSecretsFetchFail), msg)
	}

	names := make([]string, 0, len(body.Result))
	for _, s := range body.Result {
		names = append(names, s.Name)
	}
	return names, nil
}

// checkResponse はレスポンスを検証し、成功なら "" を、失敗ならエラーメッセージを返す。
// Cloudflare は HTTP 200 でも success:false を返すことがあるためボディも検証する。
// 呼び出し側でボディを Close する。
func checkResponse(res *http.Response) string {
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Sprintf("HTTP %d", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseErrorBody(data); detail != "" {
			msg += ": " + detail
		}
		return msg
	}
	var body struct {
		Success *bool `json:"success"`
	}
	// ボディが JSON でない、または success フィールドが無い場合は 2xx を成功とみなす。
	if err := json.Unmarshal(data, &body); err != nil || body.Success == nil || *body.Success {
		return ""
	}
	if detail := parseErrorBody(data); detail != "" {
		return detail
	}
	return "success=false"
}

// parseErrorBody は Cloudflare のエラーレスポンス本文から errors[].message を連結して返す。
func parseErrorBody(data []byte) string {
	var body struct {
		Errors []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return ""
	}
	msgs := make([]string, 0, len(body.Errors))
	for _, e := range body.Errors {
		switch {
		case e.Message != "":
			msgs = append(msgs, e.Message)
		case e.Code != 0:
			msgs = append(msgs, fmt.Sprintf("code %d", e.Code))
		}
	}
	return strings.Join(msgs, "; ")
}

// secretsURL は Workers script secrets エンドポイントの URL を返す。
func secretsURL(accountID, script string) string {
	return fmt.Sprintf("%s/accounts/%s/workers/scripts/%s/secrets",
		apiBase, url.PathEscape(accountID), url.PathEscape(script))
}

// wranglerConfigFiles は script 名フォールバックで探索する wrangler 設定ファイル（優先順）。
var wranglerConfigFiles = []string{"wrangler.jsonc", "wrangler.json", "wrangler.toml"}

// applyWranglerFallback は単一ターゲット時の wrangler 設定ファイルフォールバックを行う。
// targets[0].Script が空の場合に wrangler.jsonc / wrangler.json / wrangler.toml の name を読む。
// 設定ファイルが存在しない場合はエラーではなく何もしない（呼び出し側が未解決として扱う）。
func applyWranglerFallback(targets []config.CloudflareTarget) error {
	if len(targets) != 1 || targets[0].Script != "" {
		return nil
	}
	for _, path := range wranglerConfigFiles {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s", i18n.T(i18n.MsgCloudflareWranglerReadFail, path, err))
		}
		name := parseWranglerName(path, string(data))
		if name == "" {
			continue
		}
		targets[0].Script = name
		targets[0].ScriptSource = "wrangler"
		return nil
	}
	return nil
}

// parseWranglerName は wrangler 設定ファイルのテキストからトップレベルの name を抽出する純粋関数。
// path の拡張子で TOML / JSON(C) を判別する。抽出できない場合は "" を返す。
func parseWranglerName(path, text string) string {
	if strings.HasSuffix(path, ".toml") {
		return tomlTopLevelName(text)
	}
	var conf struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stripJSONComments(text)), &conf); err != nil {
		return ""
	}
	return conf.Name
}

// tomlTopLevelName は wrangler.toml のトップレベル `name = "..."` を抽出する純粋関数。
// 最初のテーブルヘッダ（[env.staging] など）以降は環境別設定のため走査しない。
// wrangler.toml の name は単純な文字列リテラルであるため、完全な TOML パーサは用いない。
func tomlTopLevelName(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			// テーブルヘッダに到達したらトップレベルは終わり
			return ""
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found || strings.TrimSpace(key) != "name" {
			continue
		}
		value = strings.TrimSpace(value)
		// 行末コメントを除去（値がクォートで閉じたあとの # 以降）
		if idx := strings.LastIndex(value, "\""); idx > 0 {
			value = value[:idx+1]
		} else if idx := strings.LastIndex(value, "'"); idx > 0 {
			value = value[:idx+1]
		}
		return strings.Trim(value, `"'`)
	}
	return ""
}

// stripJSONComments は JSONC の // 行コメントと /* */ ブロックコメントを除去する純粋関数。
// 文字列リテラル内の // や /* は除去しない（エスケープ \" も考慮する）。
func stripJSONComments(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			sb.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			sb.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(s) {
			if s[i+1] == '/' {
				// 行コメント: 改行まで読み飛ばす（改行自体は残す）
				for i < len(s) && s[i] != '\n' {
					i++
				}
				if i < len(s) {
					sb.WriteByte('\n')
				}
				continue
			}
			if s[i+1] == '*' {
				// ブロックコメント: */ まで読み飛ばす
				i += 2
				for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
					i++
				}
				i++ // ループの i++ と合わせて '/' の次へ進める
				continue
			}
		}
		sb.WriteByte(c)
	}
	return sb.String()
}
