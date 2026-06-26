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
}

func (m *mockClient) GetSecret(ctx context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
	return m.getSecret(ctx, req)
}

func (m *mockClient) CreateSecret(ctx context.Context, req *secretmanagerpb.CreateSecretRequest) (*secretmanagerpb.Secret, error) {
	return m.createSecret(ctx, req)
}

func (m *mockClient) UpdateSecret(ctx context.Context, req *secretmanagerpb.UpdateSecretRequest) (*secretmanagerpb.Secret, error) {
	return m.updateSecret(ctx, req)
}

func (m *mockClient) AddSecretVersion(ctx context.Context, req *secretmanagerpb.AddSecretVersionRequest) (*secretmanagerpb.SecretVersion, error) {
	return m.addSecretVersion(ctx, req)
}

func (m *mockClient) Close() error { return nil }

// notFoundErr は codes.NotFound の gRPC ステータスエラーを返す。
func notFoundErr() error {
	return status.Error(codes.NotFound, "not found")
}

// --- buildLabels のテスト ---

func TestBuildLabels_Empty(t *testing.T) {
	labels := buildLabels(nil)
	if labels != nil {
		t.Errorf("buildLabels(nil) = %v, want nil", labels)
	}
}

func TestBuildLabels_Single(t *testing.T) {
	labels := buildLabels([]string{"production"})
	if v, ok := labels["environment"]; !ok || v != "production" {
		t.Errorf(`labels["environment"] = %q, want "production"`, v)
	}
}

func TestBuildLabels_Multiple(t *testing.T) {
	labels := buildLabels([]string{"production", "staging"})
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
	if err := syncSecret(context.Background(), mock, "my-project", entry); err != nil {
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
	if err := syncSecret(context.Background(), mock, "my-project", entry); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if updatedLabels["environment"] != "production" {
		t.Errorf(`updatedLabels["environment"] = %q, want "production"`, updatedLabels["environment"])
	}
	if !addVersionCalled {
		t.Error("AddSecretVersion が呼ばれなかった")
	}
}

func TestSyncSecret_ExistingSecret_NoEnvironments(t *testing.T) {
	updateCalled := false

	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return &secretmanagerpb.Secret{Name: req.Name}, nil
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
	if err := syncSecret(context.Background(), mock, "my-project", entry); err != nil {
		t.Fatalf("syncSecret: %v", err)
	}
	if updateCalled {
		t.Error("UpdateSecret は呼ばれるべきではない（environments なし）")
	}
}

func TestSyncSecret_GetSecretError(t *testing.T) {
	mock := &mockClient{
		getSecret: func(_ context.Context, req *secretmanagerpb.GetSecretRequest) (*secretmanagerpb.Secret, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}
	err := syncSecret(context.Background(), mock, "my-project", provider.Entry{Key: "KEY", Value: "val", Secret: true})
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
	err := syncSecret(context.Background(), mock, "my-project", provider.Entry{Key: "KEY", Value: "val", Secret: true})
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
