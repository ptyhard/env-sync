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
func (v *vercelProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	// ---- 認証情報 / プロジェクト ----
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}
	if !opts.DryRun && appCfg.ResolveVercelToken() == "" && len(appCfg.Vercel.Projects) == 0 {
		return fmt.Errorf("VERCEL_TOKEN が未設定です（環境変数 VERCEL_TOKEN または config ファイルの vercel.token で指定してください）")
	}

	// ---- ターゲット解決 ----
	targets, err := appCfg.ResolveVercelTargets(opts.VercelProject)
	if err != nil {
		return err
	}

	// ---- 登録対象を組み立て ----
	items, err := entriesToVercelItems(entries)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: httpTimeout}

	// ---- ProjectID の解決（単一ターゲット時のみ .vercel/project.json フォールバック） ----
	if len(targets) == 1 && targets[0].ProjectID == "" {
		pjText, err := os.ReadFile(".vercel/project.json")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(".vercel/project.json の読み込みに失敗: %s", err)
		}
		if err == nil {
			var pj projectJSON
			if err := json.Unmarshal(pjText, &pj); err != nil {
				return fmt.Errorf(".vercel/project.json の JSON パースに失敗: %s", err)
			}
			targets[0].ProjectID = pj.ProjectID
			if targets[0].TeamID == "" {
				targets[0].TeamID = pj.OrgID
			}
		}
		if targets[0].ProjectID == "" {
			return fmt.Errorf("VERCEL_PROJECT_ID が未設定で .vercel/project.json もありません（先に vercel link するか指定してください）")
		}
	}
	// 複数ターゲット時は各 project_id が必須（.vercel/project.json フォールバックなし）
	if len(targets) > 1 {
		for _, tgt := range targets {
			if tgt.ProjectID == "" {
				return fmt.Errorf("Vercel プロジェクト %q の project_id が設定されていません", tgt.Name)
			}
		}
	}

	// ---- 各ターゲットに対して一覧表示と分類（dry-run も同様）----
	// perTargetClassified はターゲット順に分類結果を保持し、確認・送信フェーズで再利用する。
	// tokenMissing はトークン未設定のターゲットインデックスを記録する（複数ターゲット時の失敗集約用）。
	perTargetClassified := make([][]classifiedVercelItem, len(targets))
	tokenMissing := make([]bool, len(targets))
	for i, tgt := range targets {
		// トークン未設定チェック（per-target）
		// 単一ターゲット時は即エラー返却。複数ターゲット時は失敗として記録して残りを継続する。
		if !opts.DryRun && tgt.Token == "" {
			if len(targets) == 1 {
				return fmt.Errorf("VERCEL_TOKEN が未設定です（プロジェクト %q: 環境変数 VERCEL_TOKEN または config ファイルの token で指定してください）", tgt.Name)
			}
			tokenMissing[i] = true
			fmt.Fprintf(os.Stderr, "✗ プロジェクト %q: VERCEL_TOKEN が未設定です（このターゲットをスキップして残りを継続します）\n", tgt.Name)
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
				fmt.Fprintf(os.Stderr, "警告: 既存 key の取得に失敗したため新規/更新の分類をスキップします: %s\n", err)
			}
		}
		perTargetClassified[i] = classified

		// 登録対象を一覧表示
		targetLabel := tgt.ProjectID
		if tgt.Name != "" {
			targetLabel = tgt.Name + " (" + tgt.ProjectID + ")"
		}
		fmt.Printf("対象プロジェクト: %s  (env: %s, def: %s)\n", targetLabel, opts.Env, opts.Def)
		newCount, updateCount := countClassified(classified, len(items))
		if classified != nil {
			fmt.Printf("登録対象 %d 件 (新規 %d 件 / 更新 %d 件):\n", len(items), newCount, updateCount)
		} else {
			fmt.Printf("登録対象 %d 件 (既存は upsert で上書き):\n", len(items))
		}
		for j, it := range items {
			tj, _ := json.Marshal(it.Target)
			secretLabel := "secret=true"
			if it.Type == "plain" {
				secretLabel = "secret=false"
			}
			if classified != nil {
				marker, label := "⟳", "更新"
				if classified[j].isNew {
					marker, label = "+", "新規"
				}
				fmt.Printf("  %s %-30s (%s) environments=%s [%s]\n", marker, it.Key, secretLabel, string(tj), label)
			} else {
				fmt.Printf("  %s (%s) environments=%s\n", it.Key, secretLabel, string(tj))
			}
		}
		fmt.Println()
	}

	if len(items) == 0 {
		fmt.Println("登録対象がありません")
		return nil
	}
	if opts.DryRun {
		fmt.Println("[dry-run] 送信しません")
		return nil
	}

	// ---- 確認（更新がある場合、または分類不可の場合）----
	// 一覧表示フェーズで計算した分類結果（perTargetClassified）を再利用し API 二重呼び出しを避ける。
	// 複数ターゲット時は常に確認（安全側）。単一ターゲット時は更新有無で判定。
	needsConfirm := false
	if len(targets) > 1 {
		needsConfirm = true
	} else {
		// 単一ターゲット: 保存済み分類を再利用
		classified := perTargetClassified[0]
		_, updateCount := countClassified(classified, len(items))
		needsConfirm = classified == nil || updateCount > 0
	}
	if needsConfirm && !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("対話できない環境です。確認をスキップするには --yes を付けてください")
		}
		if len(targets) > 1 {
			fmt.Printf("上記を Vercel の %d プロジェクトに登録します（既存は上書き）。続行しますか? (y/N) ", len(targets))
		} else {
			fmt.Print("上記を Vercel に登録します（既存は上書き）。続行しますか? (y/N) ")
		}
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("中止しました")
			return nil
		}
	}

	// ---- 各ターゲットへ送信 ----
	// tokenMissing[i] が true のターゲットはトークン未設定のため送信をスキップし、失敗として集計する。
	totalOK, totalNG := 0, 0
	for i, tgt := range targets {
		if tokenMissing[i] {
			// 一覧表示フェーズで警告済み。失敗件数として集計する。
			totalNG += len(items)
			continue
		}
		targetLabel := tgt.ProjectID
		if tgt.Name != "" {
			targetLabel = tgt.Name
		}
		if len(targets) > 1 {
			fmt.Printf("\n--- プロジェクト: %s ---\n", targetLabel)
		}
		ok, ng := syncOneVercelTarget(client, tgt.Token, tgt.ProjectID, tgt.TeamID, items)
		totalOK += ok
		totalNG += ng
	}

	if len(targets) > 1 {
		fmt.Printf("\n全体完了: 成功 %d / 失敗 %d\n", totalOK, totalNG)
	} else {
		fmt.Printf("\n完了: 成功 %d / 失敗 %d\n", totalOK, totalNG)
	}
	if totalNG > 0 {
		os.Exit(1)
	}
	return nil
}

// syncOneVercelTarget は 1 つの Vercel プロジェクトへ items を送信し、成功数・失敗数を返す。
// items は呼び出し元で entriesToVercelItems 変換済みのスライス。os.Exit は呼ばない。
func syncOneVercelTarget(client *http.Client, token, projectID, teamID string, items []item) (ok, ng int) {
	u, err := url.Parse(fmt.Sprintf("%s/v10/projects/%s/env", apiBase, projectID))
	if err != nil {
		fmt.Printf("✗ URL の組み立てに失敗: %s\n", err)
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
			fmt.Printf("✗ %s -> リクエスト生成失敗: %s\n", it.Key, err)
			ng++
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			fmt.Printf("✗ %s -> 送信失敗: %s\n", it.Key, err)
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
		return nil, fmt.Errorf("URL 組み立て失敗: %w", err)
	}
	q := u.Query()
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("既存 key 取得失敗: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseErrorBody(res.Body); detail != "" {
			msg += ": " + detail
		}
		return nil, fmt.Errorf("既存 key 取得失敗: %s", msg)
	}

	var resp struct {
		Envs []struct {
			Key string `json:"key"`
		} `json:"envs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("既存 key レスポンスのパース失敗: %w", err)
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
				return nil, fmt.Errorf("%s: 不正な environments %q（production / preview / development）", e.Key, t)
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
