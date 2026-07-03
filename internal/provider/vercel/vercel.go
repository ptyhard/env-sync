// Package vercel は Vercel REST API への環境変数同期を実装する provider。
package vercel

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
	"sort"
	"strings"
	"time"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// apiBase は Vercel REST API のベース URL。テストで httptest.Server を
// 指す差し替えができるよう var にしている。
var apiBase = "https://api.vercel.com"

// httpTimeout は Vercel API 呼び出しの HTTP タイムアウト。
const httpTimeout = 30 * time.Second

func init() {
	provider.RegisterProvider("vercel", func() provider.Provider { return &vercelProvider{} })
}

// vercelProvider は Vercel への同期を担当する Provider 実装。
type vercelProvider struct{}

func (v *vercelProvider) Name() string { return "vercel" }

// Sync は Vercel への環境変数同期を行う。
// vercel.projects が設定されている場合は複数プロジェクトへ順に同期する。
// 各 Entry の VercelProjects が設定されている場合は、そのプロジェクトにのみ送信する。
func (v *vercelProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	// ---- 認証情報 / プロジェクト ----
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}
	if !opts.DryRun && appCfg.ResolveVercelToken() == "" && len(appCfg.Vercel.Projects) == 0 {
		return fmt.Errorf("%s", i18n.T(i18n.MsgVercelTokenMissing))
	}

	// ---- ターゲット解決 ----
	targets, err := appCfg.ResolveVercelTargets(opts.VercelProject)
	if err != nil {
		return err
	}

	// ---- vercel_project バリデーション ----
	if err := validateEntryVercelProjects(entries, appCfg.Vercel.Projects); err != nil {
		return err
	}

	// ---- ターゲットごとの登録対象を組み立て ----
	// vercel_project が指定された Entry はそのプロジェクトにのみ送信するためターゲット別にフィルタする。
	perTargetItems := make([][]item, len(targets))
	for i, tgt := range targets {
		targetEntries := filterEntriesByVercelProject(entries, tgt.Name)
		tgtItems, err := entriesToVercelItems(targetEntries)
		if err != nil {
			return err
		}
		perTargetItems[i] = tgtItems
	}

	client := &http.Client{Timeout: httpTimeout}

	// ---- ProjectID の解決（単一ターゲット時のみ .vercel/project.json フォールバック） ----
	if _, err := applyProjectJSONFallback(targets); err != nil {
		return err
	}
	if len(targets) == 1 && targets[0].ProjectID == "" {
		return fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectIDMissing))
	}
	// ---- 各ターゲットに対して一覧表示と分類（dry-run も同様）----
	// perTargetClassified はターゲット順に分類結果を保持し、確認・送信フェーズで再利用する。
	// perTargetPrune はターゲット順に prune 削除対象を保持する（opts.Prune が false なら常に空）。
	// tokenMissing はトークン未設定のターゲットインデックスを記録する（複数ターゲット時の失敗集約用）。
	perTargetClassified := make([][]classifiedVercelItem, len(targets))
	perTargetPrune := make([][]vercelEnv, len(targets))
	pruneKeep := opts.PruneKeep()
	tokenMissing := make([]bool, len(targets))
	for i, tgt := range targets {
		items := perTargetItems[i]

		// トークン未設定チェック（per-target）
		// 単一ターゲット時は即エラー返却。複数ターゲット時は失敗として記録して残りを継続する。
		if !opts.DryRun && tgt.Token == "" {
			if len(targets) == 1 {
				return fmt.Errorf("%s", i18n.T(i18n.MsgVercelTokenMissingProject, tgt.Name))
			}
			tokenMissing[i] = true
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgVercelTokenSkipProject, tgt.Name))
			continue
		}

		// Custom Environment slug を ID に解決する（dry-run 時はスキップ）。
		// custom slug が 1 件もないターゲットでは API を呼ばない。
		if !opts.DryRun && tgt.Token != "" {
			if err := applyCustomEnvironments(client, tgt, items); err != nil {
				return err
			}
		}

		// prune の環境フィルタに Custom Environment のIDが必要な場合に slug→ID マップを取得する。
		// filterEnvs に標準環境名以外（Custom Environment slug）が含まれる場合のみ API を呼ぶ。
		// dry-run でも prune プレビューに使うため DryRun を条件に含めない（read-only GET なので安全）。
		var slugToIDForPrune map[string]string
		if opts.Prune && len(opts.Environments) > 0 && tgt.Token != "" {
			hasCustomEnv := false
			for _, e := range opts.Environments {
				if !stdVercelEnvNames[e] {
					hasCustomEnv = true
					break
				}
			}
			if hasCustomEnv {
				if m, fetchErr := vercelFetchCustomEnvironments(client, tgt.Token, tgt.ProjectID, tgt.TeamID); fetchErr == nil {
					slugToIDForPrune = m
				}
				// 取得失敗時は slugToIDForPrune = nil のまま（custom env フィルタなし、安全側）
			}
		}

		// 既存 envs を問い合わせて新規/更新を分類し、prune 対象を計算する（結果を保存して後で再利用）
		var classified []classifiedVercelItem
		if tgt.Token != "" {
			envs, err := vercelFetchEnvs(client, tgt.Token, tgt.ProjectID, tgt.TeamID)
			if err == nil {
				classified = classifyVercelItems(items, existingKeySet(envs))
				if opts.Prune {
					perTargetPrune[i] = computeVercelPrune(envs, pruneKeep, opts.Environments, slugToIDForPrune)
				}
			} else {
				// API 失敗時は classified = nil のまま（確認スキップしない安全側フォールバック）。
				// 黙って分類をスキップすると新規/更新表示が出ない理由が分からないため警告を出す。
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgVercelExistingKeysFetchWarn, err))
				// 既存一覧が取れない場合は何を消してよいか判定できないため prune もスキップする。
				if opts.Prune {
					fmt.Fprint(os.Stderr, i18n.T(i18n.MsgPruneSkipWarn, err))
				}
			}
		}
		perTargetClassified[i] = classified

		// 登録対象を一覧表示
		targetLabel := tgt.ProjectID
		if tgt.Name != "" {
			targetLabel = tgt.Name + " (" + tgt.ProjectID + ")"
		}
		fmt.Print(i18n.T(i18n.MsgVercelTargetProject, targetLabel, opts.Env, opts.Def))
		newCount, updateCount := countClassified(classified, len(items))
		if classified != nil {
			fmt.Print(i18n.T(i18n.MsgEntriesClassified, len(items), newCount, updateCount))
		} else {
			fmt.Print(i18n.T(i18n.MsgVercelEntriesUpsert, len(items)))
		}
		for j, it := range items {
			// 一覧表示では標準環境（Target）と Custom Environment slug（customEnvSlugs）を結合して表示
			allEnvs := append(append([]string{}, it.Target...), it.customEnvSlugs...)
			tj, _ := json.Marshal(allEnvs)
			secretLabel := "secret=true"
			if it.Type == "plain" {
				secretLabel = "secret=false"
			}
			if classified != nil {
				marker, label := "⟳", i18n.T(i18n.MsgLabelUpdate)
				if classified[j].isNew {
					marker, label = "+", i18n.T(i18n.MsgLabelNew)
				}
				fmt.Printf("  %s %-30s (%s) environments=%s [%s]\n", marker, it.Key, secretLabel, string(tj), label)
			} else {
				fmt.Printf("  %s (%s) environments=%s\n", it.Key, secretLabel, string(tj))
			}
		}
		// prune 削除対象の一覧表示
		if len(perTargetPrune[i]) > 0 {
			fmt.Print(i18n.T(i18n.MsgPruneEntries, len(perTargetPrune[i])))
			for _, e := range perTargetPrune[i] {
				tj, _ := json.Marshal(e.Target)
				fmt.Printf("  - %-30s environments=%s [%s]\n", e.Key, string(tj), i18n.T(i18n.MsgLabelDelete))
			}
		}
		fmt.Println()
	}

	totalItems := 0
	for _, items := range perTargetItems {
		totalItems += len(items)
	}
	totalPrune := 0
	for _, p := range perTargetPrune {
		totalPrune += len(p)
	}
	if totalItems == 0 && totalPrune == 0 {
		fmt.Println(i18n.T(i18n.MsgNoEntries))
		return nil
	}
	if opts.DryRun {
		fmt.Println(i18n.T(i18n.MsgDryRun))
		return nil
	}

	// ---- 確認（更新がある場合、または分類不可の場合）----
	// 一覧表示フェーズで計算した分類結果（perTargetClassified）を再利用し API 二重呼び出しを避ける。
	// 複数ターゲット時は常に確認（安全側）。単一ターゲット時は更新有無で判定。
	// tokenMissing のターゲットはスキップ済みのため送信対象件数から除外する。
	activeCount := 0
	for _, m := range tokenMissing {
		if !m {
			activeCount++
		}
	}
	needsConfirm := false
	if activeCount > 1 {
		needsConfirm = true
	} else if activeCount == 1 {
		// 送信可能な単一ターゲット: 保存済み分類を再利用
		for i, m := range tokenMissing {
			if !m {
				classified := perTargetClassified[i]
				_, updateCount := countClassified(classified, len(perTargetItems[i]))
				needsConfirm = classified == nil || updateCount > 0
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
			fmt.Print(i18n.T(i18n.MsgVercelConfirmMulti, activeCount))
		} else {
			fmt.Print(i18n.T(i18n.MsgVercelConfirmSingle))
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
	// tokenMissing[i] が true のターゲットはトークン未設定のため送信をスキップし、失敗として集計する。
	totalOK, totalNG := 0, 0
	for i, tgt := range targets {
		if tokenMissing[i] {
			// 一覧表示フェーズで警告済み。失敗件数として集計する。
			totalNG += len(perTargetItems[i])
			continue
		}
		targetLabel := tgt.ProjectID
		if tgt.Name != "" {
			targetLabel = tgt.Name
		}
		if activeCount > 1 {
			fmt.Print(i18n.T(i18n.MsgVercelProjectSeparator, targetLabel))
		}
		ok, ng := syncOneVercelTarget(client, tgt.Token, tgt.ProjectID, tgt.TeamID, perTargetItems[i])
		totalOK += ok
		totalNG += ng
		// prune 削除（upsert 完了後に実行）
		if len(perTargetPrune[i]) > 0 {
			dok, dng := deleteVercelEnvs(client, tgt.Token, tgt.ProjectID, tgt.TeamID, perTargetPrune[i])
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
		os.Exit(1)
	}
	return nil
}

// filterEntriesByVercelProject は tgtName に向けて送信すべき Entry のみを返す。
// entry.VercelProjects が空なら全ターゲット向け（フィルタなし）。
// entry.VercelProjects が設定されている場合、tgtName が含まれるもののみ返す。
func filterEntriesByVercelProject(entries []provider.Entry, tgtName string) []provider.Entry {
	var result []provider.Entry
	for _, e := range entries {
		if len(e.VercelProjects) == 0 {
			result = append(result, e)
			continue
		}
		for _, p := range e.VercelProjects {
			if p == tgtName {
				result = append(result, e)
				break
			}
		}
	}
	return result
}

// validateEntryVercelProjects は entries の VercelProjects フィールドを検証する。
// vercel.projects[] が未定義（単一解決モード）の場合は VercelProjects を指定できない。
// vercel.projects[] が定義されている場合は全プロジェクト名の存在チェックを行う。
func validateEntryVercelProjects(entries []provider.Entry, projects []config.VercelProjectConf) error {
	if len(projects) == 0 {
		for _, e := range entries {
			if len(e.VercelProjects) > 0 {
				return fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectNotDefined, e.Key))
			}
		}
		return nil
	}
	validNames := make(map[string]bool, len(projects))
	for _, p := range projects {
		validNames[p.Name] = true
	}
	for _, e := range entries {
		for _, vp := range e.VercelProjects {
			if !validNames[vp] {
				names := make([]string, 0, len(projects))
				for _, p := range projects {
					names = append(names, p.Name)
				}
				return fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectInvalidConfig, e.Key, vp, strings.Join(names, ", ")))
			}
		}
	}
	return nil
}

// syncOneVercelTarget は 1 つの Vercel プロジェクトへ items を送信し、成功数・失敗数を返す。
// items は呼び出し元で entriesToVercelItems 変換済みのスライス。os.Exit は呼ばない。
func syncOneVercelTarget(client *http.Client, token, projectID, teamID string, items []item) (ok, ng int) {
	u, err := url.Parse(fmt.Sprintf("%s/v10/projects/%s/env", apiBase, projectID))
	if err != nil {
		fmt.Print(i18n.T(i18n.MsgVercelURLBuildFailOut, err))
		return 0, len(items)
	}
	q := u.Query()
	q.Set("upsert", "true")
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	for _, it := range items {
		body, _ := json.Marshal(it)
		req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgVercelRequestCreateFailOut, it.Key, err))
			ng++
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgVercelSendFailOut, it.Key, err))
			ng++
			continue
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			fmt.Printf("✓ %s (%s)\n", it.Key, it.Type)
			ok++
			io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		} else {
			msg := fmt.Sprintf("HTTP %d", res.StatusCode)
			if detail := parseErrorBody(res.Body); detail != "" {
				msg += ": " + detail
			}
			fmt.Printf("✗ %s -> %s\n", it.Key, msg)
			ng++
		}
		res.Body.Close()
	}
	return ok, ng
}

// classifiedVercelItem は item に新規/更新の分類情報を付加した型。
type classifiedVercelItem struct {
	it    item
	isNew bool // true=新規, false=更新
}

// vercelEnv は Vercel に登録済みの環境変数 1 レコード分の情報（prune の削除に ID が必要）。
type vercelEnv struct {
	ID     string   `json:"id"`
	Key    string   `json:"key"`
	Type   string   `json:"type"`
	Target []string `json:"target"`
	// CustomEnvironmentIDs は Custom Environment（staging 等）の ID リスト。
	// 標準環境（production/preview/development）は Target に入り、
	// Custom Environment は Target が空になり CustomEnvironmentIDs に ID が入る。
	CustomEnvironmentIDs []string `json:"customEnvironmentIds"`
	// ConfigurationID が非空の変数はインテグレーション（Blob Store 等）が作成・管理している。
	ConfigurationID string `json:"configurationId"`
	// System が true の変数は Vercel が自動提供するシステム変数。
	System bool `json:"system"`
}

// vercelFetchEnvs は Vercel プロジェクトに登録済みの環境変数一覧を返す。
func vercelFetchEnvs(client *http.Client, token, projectID, teamID string) ([]vercelEnv, error) {
	u, err := url.Parse(fmt.Sprintf("%s/v10/projects/%s/env", apiBase, projectID))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelURLBuildFailInternal), err)
	}
	q := u.Query()
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelExistingKeyFetchFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseErrorBody(res.Body); detail != "" {
			msg += ": " + detail
		}
		return nil, fmt.Errorf("%s: %s", i18n.T(i18n.MsgVercelExistingKeyFetchFail), msg)
	}

	var resp struct {
		Envs []vercelEnv `json:"envs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelExistingKeyParseFail), err)
	}
	return resp.Envs, nil
}

// existingKeySet は vercelFetchEnvs の結果から key 名セットを作る純粋関数。
func existingKeySet(envs []vercelEnv) map[string]bool {
	existing := make(map[string]bool, len(envs))
	for _, e := range envs {
		existing[e.Key] = true
	}
	return existing
}

// stdVercelEnvNames は Vercel の標準環境名セット（Target フィールドに入る値）。
var stdVercelEnvNames = map[string]bool{
	"production":  true,
	"preview":     true,
	"development": true,
}

// computeVercelPrune は既存 envs のうち定義ファイルに無いレコードを削除対象として返す純粋関数。
// 以下は env-sync の管理外とみなし削除対象から除外する:
//   - システム変数（system=true または type=system）
//   - インテグレーション（Blob Store・Marketplace 等）が作成した変数（configurationId が非空）
//   - ID が空のレコード（削除 URL を組み立てられないため安全側に倒して除外）
//
// keep は Options.PruneKeep が返す保持判定（定義済みキー + prune_exclude パターン）。
// filterEnvs が非空のとき、レコードの全環境（Target + CustomEnvironmentIDs）が
// filterEnvs の範囲内に収まる場合のみ削除対象にする（部分的でも範囲外を含むレコードは保持）。
// slugToID は Custom Environment の slug→ID マップ（filterEnvs に custom 名が含まれる場合に使用）。
func computeVercelPrune(envs []vercelEnv, keep func(key string) bool, filterEnvs []string, slugToID map[string]string) []vercelEnv {
	var prune []vercelEnv
	for _, e := range envs {
		if e.ID == "" || e.System || e.Type == "system" || e.ConfigurationID != "" || keep(e.Key) {
			continue
		}
		// --environments 指定時: 指定環境内に完全に収まるレコードのみ削除対象にする
		if len(filterEnvs) > 0 && !vercelEnvWithinFilter(e, filterEnvs, slugToID) {
			continue
		}
		prune = append(prune, e)
	}
	return prune
}

// vercelEnvWithinFilter は vercelEnv のすべての環境（Target + CustomEnvironmentIDs）が
// filterEnvs の範囲内かどうかを確認する純粋関数。
// true → 指定環境内に完全に収まる（削除対象候補）
// false → 指定環境外の環境を含む、または Target/CustomEnvironmentIDs が両方空（保持）
func vercelEnvWithinFilter(e vercelEnv, filterEnvs []string, slugToID map[string]string) bool {
	// Target と CustomEnvironmentIDs が両方空 → スコープ不明 → 安全側に倒して保持（削除しない）
	if len(e.Target) == 0 && len(e.CustomEnvironmentIDs) == 0 {
		return false
	}
	// 標準環境フィルタセット
	filterStd := make(map[string]bool, len(filterEnvs))
	// Custom Environment ID フィルタセット（slugToID から変換）
	filterCustomIDs := make(map[string]bool)
	for _, f := range filterEnvs {
		if stdVercelEnvNames[f] {
			filterStd[f] = true
		} else if slugToID != nil {
			if id, ok := slugToID[f]; ok {
				filterCustomIDs[id] = true
			}
		}
	}
	// Target の全環境がフィルタ内か確認
	for _, t := range e.Target {
		if !filterStd[t] {
			return false
		}
	}
	// CustomEnvironmentIDs の全 ID がフィルタ内か確認
	for _, id := range e.CustomEnvironmentIDs {
		if !filterCustomIDs[id] {
			return false
		}
	}
	return true
}

// deleteVercelEnvs は prune 対象の環境変数レコードを 1 件ずつ削除し、成功数・失敗数を返す。
// os.Exit は呼ばない。
func deleteVercelEnvs(client *http.Client, token, projectID, teamID string, envs []vercelEnv) (ok, ng int) {
	for _, e := range envs {
		u, err := url.Parse(fmt.Sprintf("%s/v9/projects/%s/env/%s", apiBase, projectID, url.PathEscape(e.ID)))
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgVercelURLBuildFailOut, err))
			ng++
			continue
		}
		q := u.Query()
		if teamID != "" {
			q.Set("teamId", teamID)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodDelete, u.String(), nil)
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgVercelRequestCreateFailOut, e.Key, err))
			ng++
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)

		res, err := client.Do(req)
		if err != nil {
			fmt.Print(i18n.T(i18n.MsgVercelSendFailOut, e.Key, err))
			ng++
			continue
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			fmt.Printf("✓ %s [%s]\n", e.Key, i18n.T(i18n.MsgLabelDelete))
			ok++
			io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		} else {
			msg := fmt.Sprintf("HTTP %d", res.StatusCode)
			if detail := parseErrorBody(res.Body); detail != "" {
				msg += ": " + detail
			}
			fmt.Printf("✗ %s -> %s\n", e.Key, msg)
			ng++
		}
		res.Body.Close()
	}
	return ok, ng
}

// vercelFetchCustomEnvironments は GET /v9/projects/{id}/custom-environments で
// slug → ID（"env_*" 形式）のマップを返す。
func vercelFetchCustomEnvironments(client *http.Client, token, projectID, teamID string) (map[string]string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/v9/projects/%s/custom-environments", apiBase, url.PathEscape(projectID)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelURLBuildFailInternal), err)
	}
	q := u.Query()
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelCustomEnvFetchFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseErrorBody(res.Body); detail != "" {
			msg += ": " + detail
		}
		return nil, fmt.Errorf("%s: %s", i18n.T(i18n.MsgVercelCustomEnvFetchFail), msg)
	}

	var resp struct {
		Environments []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"environments"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelCustomEnvParseFail), err)
	}

	m := make(map[string]string, len(resp.Environments))
	for _, e := range resp.Environments {
		m[e.Slug] = e.ID
	}
	return m, nil
}

// applyCustomEnvironments は items 内の customEnvSlugs を解決して CustomEnvironmentIDs を埋める。
// custom slug が 1 件もないターゲットでは custom-environments API を一切呼ばない。
func applyCustomEnvironments(client *http.Client, tgt config.VercelTarget, items []item) error {
	// 必要な slug を集約する（重複排除）
	slugSet := make(map[string]bool)
	for _, it := range items {
		for _, s := range it.customEnvSlugs {
			slugSet[s] = true
		}
	}
	if len(slugSet) == 0 {
		return nil
	}

	customEnvs, err := vercelFetchCustomEnvironments(client, tgt.Token, tgt.ProjectID, tgt.TeamID)
	if err != nil {
		return err
	}

	// プロジェクトラベルは Name != "" ? Name : ProjectID
	projectLabel := tgt.ProjectID
	if tgt.Name != "" {
		projectLabel = tgt.Name
	}

	for i := range items {
		// 同一 item 内での CustomEnvironmentIDs 重複を防ぐ
		addedIDs := make(map[string]bool)
		for _, slug := range items[i].customEnvSlugs {
			id, ok := customEnvs[slug]
			if !ok {
				available := make([]string, 0, len(customEnvs))
				for s := range customEnvs {
					available = append(available, s)
				}
				sort.Strings(available) // エラーメッセージを決定的にする
				var availableStr string
				if len(available) == 0 {
					availableStr = i18n.T(i18n.MsgVercelCustomEnvNoneAvailable)
				} else {
					availableStr = strings.Join(available, ", ")
				}
				return fmt.Errorf("%s", i18n.T(i18n.MsgVercelCustomEnvNotFound, slug, projectLabel, availableStr))
			}
			if !addedIDs[id] {
				addedIDs[id] = true
				items[i].CustomEnvironmentIDs = append(items[i].CustomEnvironmentIDs, id)
			}
		}
	}
	return nil
}

// classifyVercelItems は items を既存 key セットと照合して新規/更新に分類する純粋関数。
func classifyVercelItems(items []item, existingKeys map[string]bool) []classifiedVercelItem {
	result := make([]classifiedVercelItem, len(items))
	for i, it := range items {
		result[i] = classifiedVercelItem{it: it, isNew: !existingKeys[it.Key]}
	}
	return result
}

// countClassified は classified から新規件数・更新件数を返す。
// classified が nil（分類不可）のときは新規=total、更新=0 を返す。
func countClassified(classified []classifiedVercelItem, total int) (newCount, updateCount int) {
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

// entriesToVercelItems は Entry スライスを Vercel API 用の item スライスに変換する純粋関数。
// Secret=true → type:"sensitive"、false → type:"plain"
// Environments が空なら [production, preview] をデフォルト適用。
// production/preview/development は item.Target へ、それ以外は item.customEnvSlugs へ振り分ける。
// Custom Environment slug の存在確認は applyCustomEnvironments が担うため、この関数はエラーを返さない。
func entriesToVercelItems(entries []provider.Entry) ([]item, error) {
	validTargets := map[string]bool{
		"production":  true,
		"preview":     true,
		"development": true,
	}

	var items []item
	for _, e := range entries {
		typ := "sensitive"
		if !e.Secret {
			typ = "plain"
		}

		envs := e.Environments
		if len(envs) == 0 {
			envs = []string{"production", "preview"}
		}

		var target []string
		var customSlugs []string
		for _, env := range envs {
			if validTargets[env] {
				target = append(target, env)
			} else {
				customSlugs = append(customSlugs, env)
			}
		}

		items = append(items, item{
			Key:            e.Key,
			Value:          e.Value,
			Type:           typ,
			Target:         target,
			customEnvSlugs: customSlugs,
		})
	}
	return items, nil
}

// item は Vercel へ送信する 1 件の環境変数を表す。
type item struct {
	Key                  string   `json:"key"`
	Value                string   `json:"value"`
	Type                 string   `json:"type"`
	Target               []string `json:"target,omitempty"`
	CustomEnvironmentIDs []string `json:"customEnvironmentIds,omitempty"`
	customEnvSlugs       []string // 非公開: JSON化しない。表示・ID解決の中間保持用
}

// projectJSON は .vercel/project.json の必要フィールド。
type projectJSON struct {
	ProjectID string `json:"projectId"`
	OrgID     string `json:"orgId"`
}

// applyProjectJSONFallback は単一ターゲット時の .vercel/project.json フォールバックを行う。
// targets[0].ProjectID が空の場合に .vercel/project.json から取得を試みる。
// 成功時は targets[0].ProjectID / TeamID / ProjectIDSource / TeamIDSource を更新し used=true を返す。
// .vercel/project.json が存在しない場合は (false, nil) を返す（エラーではない）。
func applyProjectJSONFallback(targets []config.VercelTarget) (usedProjectJSON bool, err error) {
	if len(targets) != 1 || targets[0].ProjectID != "" {
		return false, nil
	}
	pjText, readErr := os.ReadFile(".vercel/project.json")
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectJSONReadFail, readErr))
	}
	if readErr == nil {
		var pj projectJSON
		if err := json.Unmarshal(pjText, &pj); err != nil {
			return false, fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectJSONParseFail, err))
		}
		// projectId が実際に含まれている場合のみ ProjectIDSource を更新する。
		// project.json に projectId フィールドが無い場合は source を "project_json" にしない。
		if pj.ProjectID != "" {
			targets[0].ProjectID = pj.ProjectID
			targets[0].ProjectIDSource = "project_json"
		}
		if targets[0].TeamID == "" && pj.OrgID != "" {
			targets[0].TeamID = pj.OrgID
			targets[0].TeamIDSource = "project_json"
		}
		return true, nil
	}
	return false, nil
}

// vercelCheckAccess は GET /v10/projects/{id}/env で Vercel API への到達確認を行う。
// 成功・失敗に関わらず (statusCode, detail, nil) を返す。HTTP 以外のエラーは err に返す。
func vercelCheckAccess(client *http.Client, token, projectID, teamID string) (statusCode int, detail string, err error) {
	u, err := url.Parse(fmt.Sprintf("%s/v10/projects/%s/env", apiBase, url.PathEscape(projectID)))
	if err != nil {
		return 0, "", err
	}
	q := u.Query()
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	// 2xx 以外の場合のみエラー詳細を読む。2xx 時はレスポンスボディが大きくなり得るためドレインのみ行う。
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return res.StatusCode, "", nil
	}
	d := parseErrorBody(res.Body)
	return res.StatusCode, d, nil
}

// parseErrorBody は Vercel のエラーレスポンス本文からメッセージを取り出す。
func parseErrorBody(r io.Reader) string {
	data, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return ""
	}
	if body.Error.Message != "" {
		return body.Error.Message
	}
	if body.Error.Code != "" {
		return body.Error.Code
	}
	return "unknown error"
}
