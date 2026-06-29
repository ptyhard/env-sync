// validate.go は validate サブコマンドの実装を提供する。
// 読み取り専用（GET のみ）で認証・ターゲット設定・API 到達確認を行い、書き込みは行わない。
package main

import (
	"fmt"
	"os"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// runValidate は validate サブコマンドを実行する。
// --provider で指定した provider（デフォルト: vercel）の Validator を呼び出して
// 認証トークン・ターゲット設定・API 到達確認を行う。
// def / env ファイルは使用しないため、不在でも fatal にはならない。
func runValidate(args []string, printUsage func()) error {
	printVersion := func() {
		v, c, d := versionInfo()
		fmt.Printf("env-sync version %s (commit: %s, built: %s)\n", v, c, d)
	}
	opts := config.ParseFlags(args, printUsage, printVersion)

	pname := opts.Provider
	p, ok := provider.LookupProvider(pname)
	if !ok {
		fmt.Fprint(os.Stderr, i18n.T(i18n.MsgValidateProviderUnsupported, pname))
		return nil
	}

	v, ok := p.(provider.Validator)
	if !ok {
		fmt.Fprint(os.Stderr, i18n.T(i18n.MsgValidateProviderUnsupported, pname))
		return nil
	}

	return v.Validate(opts, nil)
}
