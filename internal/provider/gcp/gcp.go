// Package gcp は GCP Secret Manager への環境変数同期を実装する provider。
package gcp

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/provider"
)

func init() {
	provider.RegisterProvider("gcp", func() provider.Provider { return &gcpProvider{} })
}

// secretManagerClient は Secret Manager API 操作を抽象化するインターフェース。
// テストでモックに差し替えられるよう分離している。
type secretManagerClient interface {
	GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error)
	CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error)
	UpdateSecret(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error)
	AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error)
	Close() error
}

// realClient は *secretmanager.Client を secretManagerClient に適合させるラッパー。
type realClient struct {
	inner *secretmanager.Client
}

func (r *realClient) GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
	return r.inner.GetSecret(ctx, req)
}

func (r *realClient) CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	return r.inner.CreateSecret(ctx, req)
}

func (r *realClient) UpdateSecret(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
	return r.inner.UpdateSecret(ctx, req)
}

func (r *realClient) AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return r.inner.AddSecretVersion(ctx, req)
}

func (r *realClient) Close() error { return r.inner.Close() }

// newClientFunc はテストで差し替え可能なクライアント生成関数。
var newClientFunc = func(ctx context.Context) (secretManagerClient, error) {
	c, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &realClient{inner: c}, nil
}

// gcpProvider は GCP Secret Manager への同期を担当する Provider 実装。
type gcpProvider struct{}

func (g *gcpProvider) Name() string { return "gcp" }

// Sync は GCP Secret Manager への環境変数同期を行う。
// Entry.Secret == false のエントリはスキップする（Secret Manager は秘匿値専用）。
func (g *gcpProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		return fmt.Errorf("GCP_PROJECT_ID が未設定です")
	}

	var secretEntries []provider.Entry
	for _, e := range entries {
		if !e.Secret {
			fmt.Printf("⚠ %s: secret=false のためスキップ（Secret Manager は秘匿値専用）\n", e.Key)
			continue
		}
		secretEntries = append(secretEntries, e)
	}

	fmt.Printf("対象プロジェクト: %s\n", projectID)
	fmt.Printf("登録対象 %d 件:\n", len(secretEntries))
	for _, e := range secretEntries {
		envsStr := strings.Join(e.Environments, ", ")
		if envsStr == "" {
			envsStr = "(labels なし)"
		}
		fmt.Printf("  %s  environments=[%s]\n", e.Key, envsStr)
	}
	fmt.Println()

	if len(secretEntries) == 0 {
		fmt.Println("登録対象がありません")
		return nil
	}

	if opts.DryRun {
		fmt.Println("[dry-run] 送信しません")
		return nil
	}

	// ---- 確認プロンプト ----
	if !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("対話できない環境です。確認をスキップするには --yes を付けてください")
		}
		fmt.Print("上記を GCP Secret Manager に同期します（新しいバージョンとして追加）。続行しますか? (y/N) ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("中止しました")
			return nil
		}
	}

	ctx := context.Background()
	client, err := newClientFunc(ctx)
	if err != nil {
		return fmt.Errorf("Secret Manager クライアントの作成に失敗: %s", err)
	}
	defer client.Close() //nolint:errcheck

	ok, ng := 0, 0
	for _, e := range secretEntries {
		if err := syncSecret(ctx, client, projectID, e); err != nil {
			fmt.Printf("✗ %s -> %s\n", e.Key, err)
			ng++
		} else {
			fmt.Printf("✓ %s\n", e.Key)
			ok++
		}
	}

	fmt.Printf("\n完了: 成功 %d / 失敗 %d\n", ok, ng)
	if ng > 0 {
		os.Exit(1)
	}
	return nil
}

// syncSecret は 1 件のエントリを Secret Manager に同期する。
// secret が存在しなければ作成し、存在すれば labels を更新する。その後バージョンを追加する。
// GetSecret → CreateSecret の間に別プロセスが同名 secret を作成した場合（AlreadyExists）は
// 競合を無視してバージョン追加へ進む（再実行・並行実行に強い挙動）。
func syncSecret(ctx context.Context, client secretManagerClient, projectID string, e provider.Entry) error {
	secretName := fmt.Sprintf("projects/%s/secrets/%s", projectID, e.Key)
	parent := fmt.Sprintf("projects/%s", projectID)

	labels := buildLabels(e.Environments)

	_, err := client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: secretName})
	if err != nil {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.NotFound {
			return fmt.Errorf("Secret の取得に失敗: %s", err)
		}
		// 存在しない場合は新規作成
		secret := &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
			Labels: labels,
		}
		if _, createErr := client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
			Parent:   parent,
			SecretId: e.Key,
			Secret:   secret,
		}); createErr != nil {
			// 競合（別プロセスが同時に作成した場合）は無視してバージョン追加へ進む
			if cst, ok2 := status.FromError(createErr); !ok2 || cst.Code() != codes.AlreadyExists {
				return fmt.Errorf("Secret の作成に失敗: %s", createErr)
			}
		}
	} else if len(labels) > 0 {
		// 既存 secret の labels を更新する
		if _, err = client.UpdateSecret(ctx, &secretmanagerpb.UpdateSecretRequest{
			Secret: &secretmanagerpb.Secret{
				Name:   secretName,
				Labels: labels,
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
		}); err != nil {
			return fmt.Errorf("Secret のラベル更新に失敗: %s", err)
		}
	}

	if _, err = client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  secretName,
		Payload: &secretmanagerpb.SecretPayload{Data: []byte(e.Value)},
	}); err != nil {
		return fmt.Errorf("Secret バージョンの追加に失敗: %s", err)
	}

	return nil
}

// buildLabels は Entry.Environments から Secret Manager labels を組み立てる。
// 複数 environment は "-" で連結して "environment" キーに格納する。
func buildLabels(environments []string) map[string]string {
	if len(environments) == 0 {
		return nil
	}
	return map[string]string{"environment": strings.Join(environments, "-")}
}
