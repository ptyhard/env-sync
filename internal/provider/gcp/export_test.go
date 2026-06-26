package gcp

import "context"

// BuildLabelsForTest は buildLabels をテストから呼び出すためのエクスポート。
var BuildLabelsForTest = buildLabels

// SyncSecretForTest は syncSecret をテストから呼び出すためのエクスポート。
var SyncSecretForTest = syncSecret

// SwapClientFunc はテストでクライアント生成関数を差し替え、復元関数を返す。
func SwapClientFunc(fn func(ctx context.Context) (secretManagerClient, error)) (restore func()) {
	orig := newClientFunc
	newClientFunc = fn
	return func() { newClientFunc = orig }
}
