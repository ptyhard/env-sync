package gcp

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ptyhard/env-sync/internal/provider"
)

// mockClient は secretManagerClient のテスト用モック実装。
type mockClient struct {
	getSecret        func(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error)
	createSecret     func(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error)
	updateSecret     func(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error)
	addSecretVersion func(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error)
	listSecrets      func(ctx context.Context, req *secretmanagerpb.ListSecretsRequest) ([]*secretmanagerpb.Secret, error)
	deleteSecret     func(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) error
}

func (m *mockClient) GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
	return m.getSecret(ctx, req)
}

func (m *mockClient) CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	return m.createSecret(ctx, req)
}

func (m *mockClient) UpdateSecret(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
	if m.updateSecret == nil {
		return &secretmanagerpb.Secret{}, nil
	}
	return m.updateSecret(ctx, req)
}

func (m *mockClient) AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return m.addSecretVersion(ctx, req)
}

func (m *mockClient) ListSecrets(ctx context.Context, req *secretmanagerpb.ListSecretsRequest) ([]*secretmanagerpb.Secret, error) {
	if m.listSecrets == nil {
		return nil, nil
	}
	return m.listSecrets(ctx, req)
}

func (m *mockClient) DeleteSecret(ctx context.Context, req *secretmanagerpb.DeleteSecretRequest) error {
	if m.deleteSecret == nil {
		return nil
	}
	return m.deleteSecret(ctx, req)
}

func (m *mockClient) Close() error { return nil }

// notFoundErr は codes.NotFound の gRPC ステータスエラーを返す。
func notFoundErr() error {
	return status.Error(codes.NotFound, "not found")
}

// --- buildLabels のテスト ---

func TestBuildLabels_Empty(t *testing.T) {
	// environments が空でも managed-by ラベルは常に付与される（prune の対象識別用）
	labels := buildLabels(nil, false)
	if v, ok := labels["managed-by"]; !ok || v != "env-sync" {
		t.Errorf(`labels["managed-by"] = %q, want "env-sync"`, v)
	}
	if _, ok := labels["environment"]; ok {
		t.Errorf(`environments が空のとき labels["environment"] は付与されないはず: %v`, labels)
	}
}

func TestBuildLabels_Single(t *testing.T) {
	labels := buildLabels([]string{"production"}, false)
	if v, ok := labels["environment"]; !ok || v != "production" {
		t.Errorf(`labels["environment"] = %q, want "production"`, v)
	}
	if v, ok := labels["managed-by"]; !ok || v != "env-sync" {
		t.Errorf(`labels["managed-by"] = %q, want "env-sync"`, v)
	}
}

func TestBuildLabels_Multiple(t *testing.T) {
	labels := buildLabels([]string{"production", "staging"}, false)
	if v, ok := labels["environment"]; !ok || v != "production-staging" {
		t.Errorf(`labels["environment"] = %q, want "production-staging"`, v)
	}
}

// --- syncSecret のテスト ---

func TestSyncSecret_NewSecret(t *testing.T) {
	var createdSecretID string
	var addedData []byte

	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return nil, notFoundErr()
		},
		createSecret: func(_ context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
			createdSecretID = req.SecretId
			return &secretmanagerpb.Secret{Name: req.Parent + "/secrets/" + req.SecretId}, nil
		},
		addSecretVersion: func(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			addedData = req.Payload.Data
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}

	entry := provider.Entry{Key: "MY_SECRET", Value: "secret-value", Secret: true}
	if err := syncSecret(context.Background(), mock, "my-project", entry, false); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if createdSecretID != "MY_SECRET" {
		t.Errorf("createdSecretID = %q, want MY_SECRET", createdSecretID)
	}
	if string(addedData) != "secret-value" {
		t.Errorf("addedData = %q, want secret-value", string(addedData))
	}
}

func TestSyncSecret_ExistingSecret_WithEnvironments(t *testing.T) {
	var updatedLabels map[string]string
	var addVersionCalled bool

	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{Name: req.Name}, nil
		},
		updateSecret: func(_ context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
			updatedLabels = req.Secret.Labels
			return &secretmanagerpb.Secret{}, nil
		},
		addSecretVersion: func(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			addVersionCalled = true
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}

	entry := provider.Entry{Key: "MY_SECRET", Value: "val", Secret: true, Environments: []string{"production"}}
	if err := syncSecret(context.Background(), mock, "my-project", entry, false); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if updatedLabels["environment"] != "production" {
		t.Errorf(`updatedLabels["environment"] = %q, want "production"`, updatedLabels["environment"])
	}
	if !addVersionCalled {
		t.Error("AddSecretVersion が呼ばれなかった")
	}
}

func TestSyncSecret_ExistingSecret_NoEnvironments_AddsManagedLabel(t *testing.T) {
	// managed-by ラベルが無い既存 secret には environments が無くてもラベル更新が入る（prune の対象識別用）
	var updatedLabels map[string]string

	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{Name: req.Name}, nil
		},
		updateSecret: func(_ context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
			updatedLabels = req.Secret.Labels
			return &secretmanagerpb.Secret{}, nil
		},
		addSecretVersion: func(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}

	entry := provider.Entry{Key: "MY_SECRET", Value: "val", Secret: true}
	if err := syncSecret(context.Background(), mock, "my-project", entry, false); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if updatedLabels["managed-by"] != "env-sync" {
		t.Errorf(`updatedLabels["managed-by"] = %q, want "env-sync"`, updatedLabels["managed-by"])
	}
}

func TestSyncSecret_ExistingSecret_LabelsUpToDate_SkipsUpdate(t *testing.T) {
	// 既存 secret のラベルが最新（managed-by 付与済み・environment 一致）なら更新をスキップする
	updateCalled := false

	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{
				Name:   req.Name,
				Labels: map[string]string{"managed-by": "env-sync", "team": "custom"},
			}, nil
		},
		updateSecret: func(_ context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
			updateCalled = true
			return &secretmanagerpb.Secret{}, nil
		},
		addSecretVersion: func(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}

	entry := provider.Entry{Key: "MY_SECRET", Value: "val", Secret: true}
	if err := syncSecret(context.Background(), mock, "my-project", entry, false); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if updateCalled {
		t.Error("UpdateSecret は呼ばれるべきではない（ラベルに差分なし）")
	}
}

func TestSyncSecret_ExistingSecret_PreservesUnrelatedLabels(t *testing.T) {
	// 既存の無関係なラベルはマージで保持される
	var updatedLabels map[string]string

	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{
				Name:   req.Name,
				Labels: map[string]string{"team": "custom"},
			}, nil
		},
		updateSecret: func(_ context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
			updatedLabels = req.Secret.Labels
			return &secretmanagerpb.Secret{}, nil
		},
		addSecretVersion: func(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}

	entry := provider.Entry{Key: "MY_SECRET", Value: "val", Secret: true, Environments: []string{"production"}}
	if err := syncSecret(context.Background(), mock, "my-project", entry, false); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if updatedLabels["team"] != "custom" {
		t.Errorf(`updatedLabels["team"] = %q, want "custom"（無関係なラベルは保持）`, updatedLabels["team"])
	}
	if updatedLabels["environment"] != "production" {
		t.Errorf(`updatedLabels["environment"] = %q, want "production"`, updatedLabels["environment"])
	}
}

func TestSyncSecret_GetSecretError(t *testing.T) {
	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}
	err := syncSecret(context.Background(), mock, "my-project", provider.Entry{Key: "KEY", Value: "val", Secret: true}, false)
	if err == nil {
		t.Error("エラーが返るべき")
	}
}

func TestSyncSecret_AddVersionError(t *testing.T) {
	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{Name: req.Name}, nil
		},
		addSecretVersion: func(_ context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			return nil, fmt.Errorf("add version failed")
		},
	}
	err := syncSecret(context.Background(), mock, "my-project", provider.Entry{Key: "KEY", Value: "val", Secret: true}, false)
	if err == nil {
		t.Error("エラーが返るべき")
	}
}

// --- gcpProvider.Sync のテスト ---

func TestSync_MissingProjectID(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "")
	p := &gcpProvider{}
	err := p.Sync(provider.Options{}, []provider.Entry{{Key: "K", Value: "v", Secret: true}})
	if err == nil {
		t.Error("GCP_PROJECT_ID 未設定でエラーが返るべき")
	}
}

func TestSync_DryRun(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "test-project")

	clientCalled := false
	restore := SwapClientFunc(func(_ context.Context) (secretManagerClient, error) {
		clientCalled = true
		return nil, nil
	})
	defer restore()

	p := &gcpProvider{}
	err := p.Sync(provider.Options{DryRun: true}, []provider.Entry{
		{Key: "MY_SECRET", Value: "v", Secret: true},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if clientCalled {
		t.Error("[dry-run] API クライアントは生成されるべきではない")
	}
}

func TestSync_SkipsNonSecretEntries(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "test-project")

	// secret=false エントリのみの場合、クライアント生成・API 呼び出しなし
	clientCalled := false
	restore := SwapClientFunc(func(_ context.Context) (secretManagerClient, error) {
		clientCalled = true
		return nil, nil
	})
	defer restore()

	p := &gcpProvider{}
	err := p.Sync(provider.Options{DryRun: true}, []provider.Entry{
		{Key: "PLAIN_VAR", Value: "val", Secret: false},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if clientCalled {
		t.Error("secret=false エントリのみのとき API クライアントは生成されるべきではない")
	}
}

func TestSync_SecretCreatedAndVersionAdded(t *testing.T) {
	t.Setenv("GCP_PROJECT_ID", "test-project")

	var createdID string
	var versionAdded bool

	mock := &mockClient{
		getSecret: func(_ context.Context, _ *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return nil, notFoundErr()
		},
		createSecret: func(_ context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
			createdID = req.SecretId
			return &secretmanagerpb.Secret{}, nil
		},
		addSecretVersion: func(_ context.Context, _ *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			versionAdded = true
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}
	restore := SwapClientFunc(func(_ context.Context) (secretManagerClient, error) { return mock, nil })
	defer restore()

	p := &gcpProvider{}
	if err := p.Sync(provider.Options{Yes: true}, []provider.Entry{
		{Key: "MY_KEY", Value: "my-val", Secret: true},
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if createdID != "MY_KEY" {
		t.Errorf("createdID = %q, want MY_KEY", createdID)
	}
	if !versionAdded {
		t.Error("AddSecretVersion が呼ばれなかった")
	}
}

// --- registry への登録確認 ---

func TestGCPProvider_Registered(t *testing.T) {
	p, ok := provider.LookupProvider("gcp")
	if !ok {
		t.Fatal("gcp provider が registry に登録されていない")
	}
	if p.Name() != "gcp" {
		t.Errorf("Name() = %q, want gcp", p.Name())
	}
}

// TestFirebaseProvider_Registered は registry 経由で firebase provider を引けることを確認する。
// 直接 gcpProvider{firebase: true} を作るテストでは、登録名や factory が壊れても気付けず
// 実際の --provider firebase が起動時に拒否されるため、registry 経由でも確認する。
func TestFirebaseProvider_Registered(t *testing.T) {
	p, ok := provider.LookupProvider("firebase")
	if !ok {
		t.Fatal("firebase provider が registry に登録されていない")
	}
	if p.Name() != "firebase" {
		t.Errorf("Name() = %q, want firebase", p.Name())
	}
}

// --- firebase プロバイダのテスト ---

func TestFirebaseProvider_Name(t *testing.T) {
	if got := (&gcpProvider{firebase: true}).Name(); got != "firebase" {
		t.Errorf("Name() = %q, want %q", got, "firebase")
	}
	if got := (&gcpProvider{}).Name(); got != "gcp" {
		t.Errorf("Name() = %q, want %q", got, "gcp")
	}
}

func TestBuildLabels_Firebase(t *testing.T) {
	// firebase functions:secrets:set と同じラベルを付ける（firebase CLI の管理判定に使われる）
	labels := buildLabels([]string{"production"}, true)
	if v := labels["firebase-managed"]; v != "functions" {
		t.Errorf(`labels["firebase-managed"] = %q, want "functions"`, v)
	}
	if v := labels["managed-by"]; v != "env-sync" {
		t.Errorf(`labels["managed-by"] = %q, want "env-sync"`, v)
	}
	// gcp では付けない（Firebase と無関係な Secret に誤ったメタデータを残さない）
	if _, ok := buildLabels([]string{"production"}, false)["firebase-managed"]; ok {
		t.Error("gcp provider で firebase-managed ラベルが付いている")
	}
}

func TestSyncSecret_FirebaseLabelOnCreate(t *testing.T) {
	var created *secretmanagerpb.Secret
	mock := &mockClient{
		getSecret: func(context.Context, *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return nil, notFoundErr()
		},
		createSecret: func(_ context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
			created = req.Secret
			return req.Secret, nil
		},
		addSecretVersion: func(context.Context, *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
			return &secretmanagerpb.SecretVersion{}, nil
		},
	}
	entry := provider.Entry{Key: "API_KEY", Value: "val", Secret: true}
	if err := syncSecret(context.Background(), mock, "my-project", entry, true); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if created.GetLabels()["firebase-managed"] != "functions" {
		t.Errorf("作成した Secret の labels = %v, firebase-managed=functions が必要", created.GetLabels())
	}
}

func TestFirebaseProvider_ProjectID(t *testing.T) {
	g := &gcpProvider{firebase: true}
	t.Setenv("GCP_PROJECT_ID", "")
	t.Setenv("FIREBASE_PROJECT_ID", "")
	if _, err := g.projectID(); err == nil {
		t.Error("どちらも未設定ならエラーが返るべき")
	}
	// GCP_PROJECT_ID へのフォールバック
	t.Setenv("GCP_PROJECT_ID", "from-gcp")
	if got, _ := g.projectID(); got != "from-gcp" {
		t.Errorf("projectID() = %q, want %q", got, "from-gcp")
	}
	// FIREBASE_PROJECT_ID が優先
	t.Setenv("FIREBASE_PROJECT_ID", "from-firebase")
	if got, _ := g.projectID(); got != "from-firebase" {
		t.Errorf("projectID() = %q, want %q", got, "from-firebase")
	}
	// gcp provider は FIREBASE_PROJECT_ID を見ない
	if got, _ := (&gcpProvider{}).projectID(); got != "from-gcp" {
		t.Errorf("gcp provider の projectID() = %q, want %q", got, "from-gcp")
	}
}
