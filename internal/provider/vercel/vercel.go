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
	// tokenMissing はトークン未設定のターゲットインデックスを記録する（複数ターゲット時の失敗集約用）。
	perTargetClassified := make([][]classifiedVercelItem, len(targets))
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

		// 既存 key を問い合わせて新規/更新を分類（結果を保存して後で再利用）
		var classified []classifiedVercelItem
		if tgt.Token != "" {
			existing, err := vercelFetchExistingKeys(client, tgt.Token, tgt.ProjectID, tgt.TeamID)
			if err == nil {
				classified = classifyVercelItems(items, existing)
			} else {
				// API 失敗時は classified = nil のまま（確認スキップしない安全側フォールバック）。
				// 黙って分類をスキップすると新規/更新表示が出ない理由が分からないため警告を出す。
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgVercelExistingKeysFetchWarn, err))
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
			tj, _ := json.Marshal(it.Target)
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
		fmt.Println()
	}

	totalItems := 0
	for _, items := range perTargetItems {
		totalItems += len(items)
	}
	if totalItems == 0 {
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

// vercelFetchExistingKeys は Vercel プロジェクトに登録済みの key 名セットを返す。
func vercelFetchExistingKeys(client *http.Client, token, projectID, teamID string) (map[string]bool, error) {
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
		Envs []struct {
			Key string `json:"key"`
		} `json:"envs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgVercelExistingKeyParseFail), err)
	}

	existing := make(map[string]bool, len(resp.Envs))
	for _, e := range resp.Envs {
		existing[e.Key] = true
	}
	return existing, nil
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
// Environments の値が production/preview/development 以外なら error を返す。
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

		target := e.Environments
		if len(target) == 0 {
			target = []string{"production", "preview"}
		}

		for _, t := range target {
			if !validTargets[t] {
				return nil, fmt.Errorf("%s", i18n.T(i18n.MsgVercelInvalidEnvironment, e.Key, t))
			}
		}

		items = append(items, item{Key: e.Key, Value: e.Value, Type: typ, Target: target})
	}
	return items, nil
}

// item は Vercel へ送信する 1 件の環境変数を表す。
type item struct {
	Key    string   `json:"key"`
	Value  string   `json:"value"`
	Type   string   `json:"type"`
	Target []string `json:"target"`
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
