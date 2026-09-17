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
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

func init() {
	provider.RegisterProvider("gcp", func() provider.Provider { return &gcpProvider{} })
	// firebase は「firebase-managed ラベルを足した gcp」でしかない。Firebase Functions 2nd gen の
	// defineSecret() が読むのは同じ GCP プロジェクトの Secret Manager 上の Secret そのもので、
	// firebase functions:secrets:set もこの Secret を作ってラベルを付けているだけ。
	// ラベルを合わせておくと firebase CLI 側からも Functions 管理の Secret として扱われる。
	provider.RegisterProvider("firebase", func() provider.Provider { return &gcpProvider{firebase: true} })
}

// secretManagerClient は Secret Manager API 操作を抽象化するインターフェース。
// テストでモックに差し替えられるよう分離している。
type secretManagerClient interface {
	GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error)
	CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error)
	UpdateSecret(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error)
	AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error)
	// ListSecrets は req にマッチする Secret を全件返す（実装側でイテレータを消化する）。
	ListSecrets(ctx context.Context, req *secretmanagerpb.ListSecretsRequest) ([]*secretmanagerpb.Secret, error)
	DeleteSecret(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) error
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

func (r *realClient) ListSecrets(ctx context.Context, req *secretmanagerpb.ListSecretsRequest) ([]*secretmanagerpb.Secret, error) {
	var secrets []*secretmanagerpb.Secret
	it := r.inner.ListSecrets(ctx, req)
	for {
		s, err := it.Next()
		if err == iterator.Done {
			return secrets, nil
		}
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, s)
	}
}

func (r *realClient) DeleteSecret(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) error {
	return r.inner.DeleteSecret(ctx, req)
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
// firebase が true のときは "firebase" プロバイダとして振る舞い、Secret に
// firebase-managed ラベルを付与し、プロジェクト ID を FIREBASE_PROJECT_ID からも解決する。
type gcpProvider struct {
	firebase bool
}

func (g *gcpProvider) Name() string {
	if g.firebase {
		return "firebase"
	}
	return "gcp"
}

// projectID は同期先プロジェクト ID を環境変数から解決する。
// Firebase プロジェクトは GCP プロジェクトそのものなので、firebase モードでは
// FIREBASE_PROJECT_ID を優先しつつ GCP_PROJECT_ID にもフォールバックする。
func (g *gcpProvider) projectID() (string, error) {
	if g.firebase {
		if v := os.Getenv("FIREBASE_PROJECT_ID"); v != "" {
			return v, nil
		}
		if v := os.Getenv("GCP_PROJECT_ID"); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s", i18n.T(i18n.MsgFirebaseProjectIDMissing))
	}
	v := os.Getenv("GCP_PROJECT_ID")
	if v == "" {
		return "", fmt.Errorf("%s", i18n.T(i18n.MsgGCPProjectIDMissing))
	}
	return v, nil
}

// Sync は GCP Secret Manager への環境変数同期を行う。
// Entry.Secret == false のエントリはスキップする（Secret Manager は秘匿値専用）。
func (g *gcpProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	projectID, err := g.projectID()
	if err != nil {
		return err
	}

	var secretEntries []provider.Entry
	for _, e := range entries {
		if !e.Secret {
			fmt.Print(i18n.T(i18n.MsgGCPSkipNotSecret, e.Key))
			continue
		}
		secretEntries = append(secretEntries, e)
	}

	// ---- prune 削除対象の収集（一覧表示の前に行う） ----
	// prune は managed-by=env-sync ラベル付き Secret のみを対象にする（無関係な Secret の誤削除防止）。
	ctx := context.Background()
	var client secretManagerClient
	defer func() {
		if client != nil {
			client.Close() //nolint:errcheck
		}
	}()
	var pruneIDs []string
	if opts.Prune {
		c, err := newClientFunc(ctx)
		if err != nil {
			// dry-run は認証情報なしでも動く仕様のため、警告のうえ prune 表示のみスキップする
			if !opts.DryRun {
				return fmt.Errorf("%s", i18n.T(i18n.MsgGCPClientCreateFail, err))
			}
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgPruneSkipWarn, err))
		} else {
			client = c
			secrets, err := client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
				Parent: "projects/" + projectID,
				Filter: fmt.Sprintf("labels.%s=%s", managedByLabelKey, managedByLabelValue),
			})
			if err != nil {
				// 一覧が取れない場合は何を消してよいか判定できないため削除をスキップする
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgPruneSkipWarn, err))
			} else {
				pruneIDs = computeGCPPrune(secrets, opts.PruneKeep())
			}
		}
	}

	fmt.Print(i18n.T(i18n.MsgGCPTargetProject, projectID))
	fmt.Print(i18n.T(i18n.MsgEntriesCount, len(secretEntries)))
	for _, e := range secretEntries {
		envsStr := strings.Join(e.Environments, ", ")
		if envsStr == "" {
			envsStr = i18n.T(i18n.MsgGCPLabelsNone)
		}
		fmt.Printf("  %s  environments=[%s]\n", e.Key, envsStr)
	}
	// prune 削除対象の一覧表示
	if len(pruneIDs) > 0 {
		fmt.Print(i18n.T(i18n.MsgPruneEntries, len(pruneIDs)))
		for _, id := range pruneIDs {
			fmt.Printf("  - %s [%s]\n", id, i18n.T(i18n.MsgLabelDelete))
		}
	}
	fmt.Println()

	if len(secretEntries) == 0 && len(pruneIDs) == 0 {
		fmt.Println(i18n.T(i18n.MsgNoEntries))
		return nil
	}

	if opts.DryRun {
		fmt.Println(i18n.T(i18n.MsgDryRun))
		return nil
	}

	// ---- 確認プロンプト ----
	if len(pruneIDs) > 0 {
		fmt.Print(i18n.T(i18n.MsgPruneConfirmNote, len(pruneIDs)))
	}
	if !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("%s", i18n.T(i18n.MsgNonInteractiveErr))
		}
		fmt.Print(i18n.T(i18n.MsgGCPConfirm))
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println(i18n.T(i18n.MsgAborted))
			return nil
		}
	}

	if client == nil {
		c, err := newClientFunc(ctx)
		if err != nil {
			return fmt.Errorf("%s", i18n.T(i18n.MsgGCPClientCreateFail, err))
		}
		client = c
	}

	ok, ng := 0, 0
	for _, e := range secretEntries {
		if err := syncSecret(ctx, client, projectID, e, g.firebase); err != nil {
			fmt.Printf("✗ %s -> %s\n", e.Key, err)
			ng++
		} else {
			fmt.Printf("✓ %s\n", e.Key)
			ok++
		}
	}

	// ---- prune 削除（同期完了後に実行） ----
	for _, id := range pruneIDs {
		name := fmt.Sprintf("projects/%s/secrets/%s", projectID, id)
		if err := client.DeleteSecret(ctx, &secretmanagerpb.DeleteSecretRequest{Name: name}); err != nil {
			fmt.Printf("✗ %s -> %s\n", id, err)
			ng++
		} else {
			fmt.Printf("✓ %s [%s]\n", id, i18n.T(i18n.MsgLabelDelete))
			ok++
		}
	}

	fmt.Print(i18n.T(i18n.MsgCompleted, ok, ng))
	if ng > 0 {
		os.Exit(1)
	}
	return nil
}

// computeGCPPrune は managed-by=env-sync ラベル付き Secret のうち保持対象でないものの
// Secret ID を返す純粋関数。Secret.Name は "projects/<番号>/secrets/<ID>" 形式。
// keep は Options.PruneKeep が返す保持判定（定義済みキー + prune_exclude パターン）。
func computeGCPPrune(secrets []*secretmanagerpb.Secret, keep func(key string) bool) []string {
	var ids []string
	for _, s := range secrets {
		name := s.GetName()
		id := name[strings.LastIndex(name, "/")+1:]
		if id == "" || keep(id) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// syncSecret は 1 件のエントリを Secret Manager に同期する。
// secret が存在しなければ作成し、存在すれば labels を更新する。その後バージョンを追加する。
// GetSecret → CreateSecret の間に別プロセスが同名 secret を作成した場合（AlreadyExists）は
// 競合を無視してバージョン追加へ進む（再実行・並行実行に強い挙動）。
func syncSecret(ctx context.Context, client secretManagerClient, projectID string, e provider.Entry, firebase bool) error {
	secretName := fmt.Sprintf("projects/%s/secrets/%s", projectID, e.Key)
	parent := fmt.Sprintf("projects/%s", projectID)

	labels := buildLabels(e.Environments, firebase)

	existing, err := client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: secretName})
	if err != nil {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.NotFound {
			return fmt.Errorf("%s", i18n.T(i18n.MsgGCPSecretGetFail, err))
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
				return fmt.Errorf("%s", i18n.T(i18n.MsgGCPSecretCreateFail, createErr))
			}
		}
	} else {
		// 既存 secret の labels に env-sync 管理ラベルをマージして更新する。
		// 既存の無関係なラベルは保持し、差分が無ければ更新をスキップする。
		merged := mergeLabels(existing.GetLabels(), labels)
		if !labelsEqual(existing.GetLabels(), merged) {
			if _, err = client.UpdateSecret(ctx, &secretmanagerpb.UpdateSecretRequest{
				Secret: &secretmanagerpb.Secret{
					Name:   secretName,
					Labels: merged,
				},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
			}); err != nil {
				return fmt.Errorf("%s", i18n.T(i18n.MsgGCPSecretLabelUpdateFail, err))
			}
		}
	}

	if _, err = client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  secretName,
		Payload: &secretmanagerpb.SecretPayload{Data: []byte(e.Value)},
	}); err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.MsgGCPSecretVersionAddFail, err))
	}

	return nil
}

// managedByLabelKey / managedByLabelValue は env-sync が管理する Secret を示すラベル。
// prune はこのラベルが付いた Secret のみを削除対象にする（無関係な Secret の誤削除防止）。
const (
	managedByLabelKey   = "managed-by"
	managedByLabelValue = "env-sync"
)

// firebaseManagedLabelKey / firebaseManagedLabelValue は firebase functions:secrets:set が
// 付けるのと同じラベル。firebase CLI はこのラベルで Functions 管理の Secret を判定するため、
// 値も CLI 現行版に合わせる（旧版は "true" を書いていたが CLI はどちらも受け付ける）。
const (
	firebaseManagedLabelKey   = "firebase-managed"
	firebaseManagedLabelValue = "functions"
)

// buildLabels は Entry.Environments から Secret Manager labels を組み立てる。
// managed-by=env-sync を常に付与し、複数 environment は "-" で連結して "environment" キーに格納する。
// firebase が true のときは firebase-managed=functions も付与する。
func buildLabels(environments []string, firebase bool) map[string]string {
	labels := map[string]string{managedByLabelKey: managedByLabelValue}
	if len(environments) > 0 {
		labels["environment"] = strings.Join(environments, "-")
	}
	if firebase {
		labels[firebaseManagedLabelKey] = firebaseManagedLabelValue
	}
	return labels
}

// mergeLabels は既存ラベルに env-sync 管理ラベルを重ねた新しい map を返す純粋関数。
// 既存の無関係なラベルは保持し、同名キーのみ上書きする。
func mergeLabels(existing, managed map[string]string) map[string]string {
	merged := make(map[string]string, len(existing)+len(managed))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range managed {
		merged[k] = v
	}
	return merged
}

// labelsEqual は 2 つのラベル map が等しいかを返す純粋関数。
func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
