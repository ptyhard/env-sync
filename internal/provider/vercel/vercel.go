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

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/provider"
)

const apiBase = "https://api.vercel.com"

func init() {
	provider.RegisterProvider("vercel", func() provider.Provider { return &vercelProvider{} })
}

// vercelProvider は Vercel への同期を担当する Provider 実装。
type vercelProvider struct{}

func (v *vercelProvider) Name() string { return "vercel" }

// Sync は Vercel への環境変数同期を行う。
func (v *vercelProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	// ---- 認証情報 / プロジェクト ----
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}
	token := appCfg.ResolveVercelToken()
	projectID := appCfg.ResolveVercelProjectID()
	teamID := appCfg.ResolveVercelTeamID()
	if projectID == "" {
		pjText, err := os.ReadFile(".vercel/project.json")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(".vercel/project.json の読み込みに失敗: %s", err)
		}
		if err == nil {
			var pj projectJSON
			if err := json.Unmarshal(pjText, &pj); err != nil {
				return fmt.Errorf(".vercel/project.json の JSON パースに失敗: %s", err)
			}
			projectID = pj.ProjectID
			if teamID == "" {
				teamID = pj.OrgID
			}
		}
	}
	if !opts.DryRun && token == "" {
		return fmt.Errorf("VERCEL_TOKEN が未設定です（環境変数 VERCEL_TOKEN または config ファイルの vercel.token で指定してください）")
	}
	if projectID == "" {
		return fmt.Errorf("VERCEL_PROJECT_ID が未設定で .vercel/project.json もありません（先に vercel link するか指定してください）")
	}

	// ---- 登録対象を組み立て ----
	items, err := entriesToVercelItems(entries)
	if err != nil {
		return err
	}

	// ---- 登録対象を一覧表示 ----
	fmt.Printf("対象プロジェクト: %s  (env: %s, def: %s)\n", projectID, opts.Env, opts.Def)
	fmt.Printf("登録対象 %d 件 (既存は upsert で上書き):\n", len(items))
	for _, it := range items {
		tj, _ := json.Marshal(it.Target)
		secretLabel := "secret=true"
		if it.Type == "plain" {
			secretLabel = "secret=false"
		}
		fmt.Printf("  %s (%s) environments=%s\n", it.Key, secretLabel, string(tj))
	}
	fmt.Println()

	if len(items) == 0 {
		fmt.Println("登録対象がありません")
		return nil
	}
	if opts.DryRun {
		fmt.Println("[dry-run] 送信しません")
		return nil
	}

	// ---- 確認 ----
	if !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("対話できない環境です。確認をスキップするには --yes を付けてください")
		}
		fmt.Print("上記を Vercel に登録します（既存は上書き）。続行しますか? (y/N) ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("中止しました")
			return nil
		}
	}

	// ---- 送信 ----
	u, err := url.Parse(fmt.Sprintf("%s/v10/projects/%s/env", apiBase, projectID))
	if err != nil {
		return fmt.Errorf("URL の組み立てに失敗: %s", err)
	}
	q := u.Query()
	q.Set("upsert", "true")
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	client := &http.Client{}
	ok, ng := 0, 0
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

	fmt.Printf("\n完了: 成功 %d / 失敗 %d\n", ok, ng)
	if ng > 0 {
		os.Exit(1)
	}
	return nil
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
