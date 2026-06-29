package i18n

// jaCatalog は日本語メッセージカタログ。
var jaCatalog = map[MsgKey]string{
	// ----- CLI フラグ共通 -----
	MsgFlagNeedsValue:      "エラー: %s には値が必要です\n",
	MsgFlagNeedsNonEmpty:   "エラー: %s には空でない値が必要です\n",
	MsgFlagUnknown:         "エラー: 不明な引数: %s\n",
	MsgFlagProviderInvalid: "エラー: --provider には %s を指定してください\n",
	MsgFlagOrSeparator:     " または ",

	// ----- Main / run() -----
	MsgErrorPrefix:      "エラー: %s\n",
	MsgEnvFileNotFound:  "env ファイルが見つかりません: %s",
	MsgDefFileNotFound:  "定義ファイルが見つかりません: %s",
	MsgEnvFileReadFail:  "env ファイルの読み込みに失敗: %s",
	MsgDefFileReadFail:  "定義ファイルの読み込みに失敗: %s",
	MsgDefFileYAMLFail:  "定義ファイルの YAML パースに失敗: %s",
	MsgSkipNoValueInEnv: "⚠ %s: 定義にあるが %s に値が無いためスキップ\n",
	MsgSkipNotDefined:   "⚠ %s: %s にあるが定義に無いためスキップ\n",
	MsgUsage: `env-sync - 定義ファイルで宣言した環境変数を Vercel または GitHub Actions へ一括登録(同期)する

サブコマンド:
  init      .env から env-sync.yaml の雛形を生成する
  setup     認証情報 config ファイル（.env-sync.config.yaml / ~/.config/env-sync/config.yaml）を対話生成する
  validate  token / projectId / repo の設定確認と API 到達確認（読み取り専用、書き込みなし）

使い方:
  VERCEL_TOKEN=xxxxx env-sync [オプション]
  GITHUB_TOKEN=xxxxx env-sync --provider github [オプション]
  env-sync init [--env <file>] [--def <file>] [--force]
  env-sync setup [--global] [--force]

オプション（同期）:
  --provider <name>         同期先（デフォルト vercel）
  --env <file>              値を読む env ファイル（デフォルト .env）
  --def <file>              定義 YAML（デフォルト env-sync.yaml）
  --dry-run                 送信せず新規/更新の区別を含む登録予定一覧を表示（値は出さない）
  --yes, -y                 更新(上書き)を含む場合の確認をスキップして送信
  --vercel-project <name>   config の vercel.projects から指定名のプロジェクトのみ同期（モノレポ対応）
  --github-repo <name>      config の github.repos から指定名のリポジトリのみ同期（モノレポ対応）
  --lang <code>             表示言語（en / ja、デフォルト en）
  --version                 バージョン情報を表示して終了
  -h, --help                このヘルプを表示

確認動作:
  送信前に provider へ問い合わせ、既存 key を「⟳ KEY [更新]」、未登録 key を「+ KEY [新規]」として表示する。
  更新(上書き)対象がある場合のみ確認プロンプトが出る。新規のみなら確認なしで送信する。
  非対話環境(TTY なし)で更新対象があり --yes/-y がない場合はエラーで停止し --yes を案内する。

オプション（init）:
  --env <file>   読み込む env ファイル（デフォルト .env）
  --def <file>   出力する YAML ファイル（デフォルト env-sync.yaml）
  --force        既存の def ファイルを上書きする

オプション（setup）:
  --global       ~/.config/env-sync/config.yaml（XDG_CONFIG_HOME 尊重）へ出力（デフォルトは .env-sync.config.yaml）
  --force        既存の config ファイルを上書きする

環境変数（Vercel）:
  VERCEL_TOKEN       Vercel のアクセストークン（必須、dry-run 時は不要）
  VERCEL_PROJECT_ID  プロジェクト ID。未指定なら config ファイルまたは .vercel/project.json から取得
  VERCEL_TEAM_ID     チーム(Org) ID。未指定なら config ファイルまたは .vercel/project.json の orgId

環境変数（GitHub）:
  GITHUB_TOKEN  GitHub のアクセストークン（必須、dry-run 時は不要）
  GITHUB_REPO   owner/repo 形式のリポジトリ名（未指定なら config ファイルまたは git remote origin から取得）

環境変数（GCP）:
  GCP_PROJECT_ID  Secret Manager の対象 GCP プロジェクト ID（必須）
  認証: Application Default Credentials（ADC）を使用。
        GOOGLE_APPLICATION_CREDENTIALS でサービスアカウント鍵を指定、
        または gcloud auth application-default login で ADC を設定する。

環境変数（言語）:
  ENV_SYNC_LANG  表示言語コード（en / ja）。--lang フラグより低優先。

config ファイル（環境変数の代替）:
  解決優先順位: 環境変数 > project config > global config > 既存フォールバック
  global:  ~/.config/env-sync/config.yaml  (XDG_CONFIG_HOME を尊重)
  project: .env-sync.config.yaml           (カレントディレクトリ)

  スキーマ（単一プロジェクト）:
    vercel:
      token:      <Vercel トークン>
      project_id: <プロジェクト ID>
      team_id:    <チーム ID>
    github:
      token: <GitHub トークン>
      repo:  <owner/repo>

  スキーマ（モノレポ: 複数プロジェクト / 複数リポジトリ）:
    vercel:
      token: <デフォルトトークン（per-project で上書き可）>
      team_id: <デフォルトチーム ID>
      projects:
        - name: app-a
          project_id: <プロジェクト ID>
        - name: app-b
          project_id: <プロジェクト ID>
          token: <per-project トークン（任意）>
          team_id: <per-project チーム ID（任意）>
    github:
      token: <デフォルトトークン（per-repo で上書き可）>
      repos:
        - name: frontend
          repo: org/frontend
        - name: backend
          repo: org/backend
          token: <per-repo トークン（任意）>

  ※ global config にトークンが含まれていてパーミッションが 0600 でない場合は警告を出力します

YAML スキーマ（定義ファイル env-sync.yaml）:
  secret: true|false  シークレットとして登録するか（デフォルト true）
                      Vercel: true→sensitive / false→plain
                      GitHub: true→Secret / false→Variable
  environments: []    登録先環境の配列
                      Vercel: production|preview|development（空なら production,preview）
                      GitHub: named environment 名（空なら repo レベル）
                      ※ GitHub の named environment は事前に作成が必要
  language: ja        表示言語（en / ja）。--lang フラグ・ENV_SYNC_LANG より低優先。
`,

	// ----- 共通同期メッセージ -----
	MsgNoEntries:         "登録対象がありません",
	MsgDryRun:            "[dry-run] 送信しません",
	MsgNonInteractiveErr: "対話できない環境です。確認をスキップするには --yes を付けてください",
	MsgAborted:           "中止しました",
	MsgCompleted:         "\n完了: 成功 %d / 失敗 %d\n",
	MsgTotalCompleted:    "\n全体完了: 成功 %d / 失敗 %d\n",
	MsgEntriesClassified: "登録対象 %d 件 (新規 %d 件 / 更新 %d 件):\n",
	MsgEntriesCount:      "登録対象 %d 件:\n",
	MsgLabelUpdate:       "更新",
	MsgLabelNew:          "新規",
	MsgRequestCreateFail: "リクエスト生成失敗",
	MsgSendFail:          "送信失敗",
	MsgRequestFail:       "リクエスト失敗",

	// ----- Config: ProviderVal -----
	MsgProviderStringOrArray: "provider は文字列または文字列の配列で指定してください",

	// ----- Config: AppConfig -----
	MsgConfigReadFail:             "config ファイルの読み込みに失敗 (%s)",
	MsgConfigYAMLFail:             "config ファイルの YAML パースに失敗 (%s)",
	MsgConfigEnvRefUnset:          "config で参照された環境変数が未設定または書式不正です: %s",
	MsgConfigEmptyVarName:         "(空の変数名)",
	MsgConfigPermWarning:          "警告: %s にトークンが含まれていますがパーミッションが %04o です。`chmod 0600 %s` で修正することを推奨します\n",
	MsgVercelProjectsNotDefined:   "--vercel-project が指定されましたが config に vercel.projects が定義されていません",
	MsgVercelProjectNameNotFound:  "指定されたプロジェクト名 %q が config に定義されていません",
	MsgVercelProjectNameRequired:  "vercel.projects の各エントリには name が必須です",
	MsgVercelProjectNameDuplicate: "vercel.projects の name %q が重複しています",
	MsgVercelProjectIDRequired:    "vercel.projects のエントリ %q には project_id が必須です",
	MsgGitHubReposNotDefined:      "--github-repo が指定されましたが config に github.repos が定義されていません",
	MsgGitHubRepoNameNotFound:     "指定されたリポジトリ名 %q が config に定義されていません",
	MsgGitHubRepoNameRequired:     "github.repos の各エントリには name が必須です",
	MsgGitHubRepoNameDuplicate:    "github.repos の name %q が重複しています",
	MsgGitHubRepoRepoRequired:     "github.repos のエントリ %q には repo が必須です",

	// ----- Init サブコマンド -----
	MsgInitEnvFileReadFail: "env ファイルの読み込みに失敗: %s: %s",
	MsgFileExists:          "既に存在します: %s（上書きするには --force）",
	MsgInitDefWriteFail:    "定義ファイルの書き込みに失敗: %s: %s",
	MsgGenerated:           "生成しました: %s\n",
	MsgInitKeyCount:        "キー数: %d\n",
	MsgInitKeyListHeader:   "キー一覧:\n",
	MsgInitSecretNote:      "※ secret は投入前に必ず見直してください。値はファイルに書かれていません。",
	MsgInitYAMLHeader: "# Vercel / GitHub Actions に登録する環境変数の定義。\n" +
		"#\n" +
		"# 値はこのファイルには書かない（git にコミットされるため）。値は .env(.production) から取得する。\n" +
		"# ここに宣言が無いキーは登録されない（.env にあっても警告のうえスキップされる）。\n" +
		"#\n" +
		"#   secret: true|false\n" +
		"#           - true  : シークレットとして登録（Vercel: sensitive / GitHub: Secret）\n" +
		"#           - false : 平文として登録（Vercel: plain / GitHub: Variable）\n" +
		"#   environments: []  登録先環境の配列\n" +
		"#           Vercel: production|preview|development（空なら production,preview）\n" +
		"#           GitHub: named environment 名（空なら repo レベル）\n" +
		"#\n" +
		"# !! 以下は init が生成した雛形です。secret は投入前に必ず見直すこと !!\n" +
		"# !! NEXT_PUBLIC_ プレフィックスは secret: false、それ以外は secret: true を初期値としています。!!\n",
	MsgInitYAMLExample: "  # ---- 例 ----\n" +
		"  # NEXT_PUBLIC_API_BASE_URL: { secret: false }\n" +
		"  # DATABASE_URL:             { secret: true }\n" +
		"  # STAGING_KEY:              { secret: true, environments: [production] }\n",

	// ----- Setup サブコマンド -----
	MsgSetupNonInteractive: "非対話環境（TTY なし）では setup は実行できません。\n" +
		"config ファイルを手で作成するか、次の例を参考にしてください:\n" +
		"  mkdir -p ~/.config/env-sync\n" +
		"  cat > ~/.config/env-sync/config.yaml <<'EOF'\n" +
		"  vercel:\n" +
		"    token: ${VERCEL_TOKEN}\n" +
		"    project_id: <プロジェクト ID>\n" +
		"  github:\n" +
		"    token: ${GITHUB_TOKEN}\n" +
		"    repo: <owner/repo>\n" +
		"  EOF\n" +
		"  chmod 0600 ~/.config/env-sync/config.yaml",
	MsgSetupHomeDirFail:       "ホームディレクトリが取得できません",
	MsgSetupDirCreateFail:     "ディレクトリの作成に失敗: %s: %s",
	MsgSetupConfigWriteFail:   "config ファイルの書き込みに失敗: %s: %s",
	MsgSetupConfigChmodFail:   "config ファイルのパーミッション設定に失敗: %s: %s",
	MsgSetupInputReadFail:     "入力の読み込みに失敗",
	MsgSetupTokenNote:         "トークンが含まれます。パーミッションは 0600 です。",
	MsgSetupGitignoreNote:     "※ このファイルを .gitignore に追記することを推奨します。",
	MsgSetupAskVercel:         "Vercel を設定しますか？ (Y/n): ",
	MsgSetupVercelProjectID:   "Vercel project_id: ",
	MsgSetupVercelTeamID:      "Vercel team_id（任意、Enter でスキップ）: ",
	MsgSetupVercelTokenEnvRef: "Vercel token を ${VERCEL_TOKEN} 環境変数参照で書きますか？（推奨）(Y/n): ",
	MsgSetupVercelTokenPlain:  "Vercel token（平文でファイルに書き込みます）: ",
	MsgSetupAskGitHub:         "GitHub を設定しますか？ (Y/n): ",
	MsgSetupGitHubRepo:        "GitHub repo（owner/repo 形式）: ",
	MsgSetupGitHubTokenEnvRef: "GitHub token を ${GITHUB_TOKEN} 環境変数参照で書きますか？（推奨）(Y/n): ",
	MsgSetupGitHubTokenPlain:  "GitHub token（平文でファイルに書き込みます）: ",
	MsgSetupYAMLHeader: "# env-sync 認証情報 config\n" +
		"# このファイルをコミットしないよう .gitignore に追記することを推奨します。\n" +
		"# projects[] / repos[]（モノレポ向け複数ターゲット）は手で追記してください。\n",

	// ----- Vercel Provider -----
	MsgVercelTokenMissing:          "VERCEL_TOKEN が未設定です（環境変数 VERCEL_TOKEN または config ファイルの vercel.token で指定してください）",
	MsgVercelTokenMissingProject:   "VERCEL_TOKEN が未設定です（プロジェクト %q: 環境変数 VERCEL_TOKEN または config ファイルの token で指定してください）",
	MsgVercelTokenSkipProject:      "✗ プロジェクト %q: VERCEL_TOKEN が未設定です（このターゲットをスキップして残りを継続します）\n",
	MsgVercelExistingKeysFetchWarn: "警告: 既存 key の取得に失敗したため新規/更新の分類をスキップします: %s\n",
	MsgVercelTargetProject:         "対象プロジェクト: %s  (env: %s, def: %s)\n",
	MsgVercelEntriesUpsert:         "登録対象 %d 件 (既存は upsert で上書き):\n",
	MsgVercelConfirmMulti:          "上記を Vercel の %d プロジェクトに登録します（既存は上書き）。続行しますか? (y/N) ",
	MsgVercelConfirmSingle:         "上記を Vercel に登録します（既存は上書き）。続行しますか? (y/N) ",
	MsgVercelProjectSeparator:      "\n--- プロジェクト: %s ---\n",
	MsgVercelURLBuildFailOut:       "✗ URL の組み立てに失敗: %s\n",
	MsgVercelRequestCreateFailOut:  "✗ %s -> リクエスト生成失敗: %s\n",
	MsgVercelSendFailOut:           "✗ %s -> 送信失敗: %s\n",
	MsgVercelProjectJSONReadFail:   ".vercel/project.json の読み込みに失敗: %s",
	MsgVercelProjectJSONParseFail:  ".vercel/project.json の JSON パースに失敗: %s",
	MsgVercelProjectIDMissing:      "VERCEL_PROJECT_ID が未設定で .vercel/project.json もありません（先に vercel link するか指定してください）",
	MsgVercelProjectNotDefined:     "%s: vercel_project が指定されていますが config に vercel.projects が定義されていません（vercel_project は vercel.projects[] と組み合わせて使用してください）",
	MsgVercelProjectInvalidConfig:  "%s: vercel_project %q は config の vercel.projects に存在しません（定義済み: %s）",
	MsgVercelInvalidEnvironment:    "%s: 不正な environments %q（production / preview / development）",
	MsgVercelURLBuildFailInternal:  "URL 組み立て失敗",
	MsgVercelExistingKeyFetchFail:  "既存 key 取得失敗",
	MsgVercelExistingKeyParseFail:  "既存 key レスポンスのパース失敗",

	// ----- GitHub Provider -----
	MsgGitHubTokenMissing:          "GITHUB_TOKEN が未設定です（環境変数 GITHUB_TOKEN または config ファイルの github.token で指定してください）",
	MsgGitHubTokenMissingRepo:      "GITHUB_TOKEN が未設定です（リポジトリ %q: 環境変数 GITHUB_TOKEN または config ファイルの token で指定してください）",
	MsgGitHubTokenSkipRepo:         "✗ リポジトリ %q: GITHUB_TOKEN が未設定です（このターゲットをスキップして残りを継続します）\n",
	MsgGitHubExistingCheckWarn:     "警告: 既存の存在確認に失敗したため新規/更新の分類をスキップします: %s\n",
	MsgGitHubTargetRepo:            "対象リポジトリ: %s/%s\n",
	MsgGitHubConfirmMulti:          "上記を GitHub の %d リポジトリに登録します（既存は上書き）。続行しますか? (y/N) ",
	MsgGitHubConfirmSingle:         "上記を GitHub に登録します。続行しますか? (y/N) ",
	MsgGitHubRepoSeparator:         "\n--- リポジトリ: %s/%s ---\n",
	MsgGitHubPublicKeyFetchFailOut: "✗ %s -> 公開鍵取得失敗: %s\n",
	MsgGitHubEncryptFailOut:        "✗ %s -> 暗号化失敗: %s\n",
	MsgGitHubExistCheckFailOut:     "✗ %s (env: %s) -> 存在確認失敗: %s\n",
	MsgGitHubExistCheckTaskFail:    "%s (env: %s): 存在確認失敗",
	MsgGitHubRepoFormatInvalid:     "リポジトリの形式が不正です（owner/repo 形式で指定してください）: %q",
	MsgGitHubRepoEnvInvalid:        "GITHUB_REPO の形式が不正です（owner/repo 形式で指定してください）",
	MsgGitHubRepoRequired:          "GITHUB_REPO を指定してください（git remote origin が GitHub でないか、git が使えません）",
	MsgSealedBoxEncryptFail:        "sealed box 暗号化失敗",
	MsgPublicKeyFetchFail:          "公開鍵取得失敗",
	MsgPublicKeyParseFail:          "公開鍵レスポンスのパース失敗",
	MsgPublicKeyDecodeFail:         "公開鍵の base64 デコード失敗",
	MsgPublicKeyLengthInvalid:      "公開鍵の長さが不正です（%d バイト、32 バイト必要）",
	MsgSecretExistCheckFail:        "シークレットの存在確認失敗: %s",
	MsgVariableExistCheckFail:      "変数の存在確認失敗: %s",

	// ----- GCP Provider -----
	MsgGCPProjectIDMissing:      "GCP_PROJECT_ID が未設定です",
	MsgGCPSkipNotSecret:         "⚠ %s: secret=false のためスキップ（Secret Manager は秘匿値専用）\n",
	MsgGCPTargetProject:         "対象プロジェクト: %s\n",
	MsgGCPLabelsNone:            "(labels なし)",
	MsgGCPConfirm:               "上記を GCP Secret Manager に同期します（新しいバージョンとして追加）。続行しますか? (y/N) ",
	MsgGCPClientCreateFail:      "Secret Manager クライアントの作成に失敗: %s",
	MsgGCPSecretGetFail:         "Secret の取得に失敗: %s",
	MsgGCPSecretCreateFail:      "Secret の作成に失敗: %s",
	MsgGCPSecretLabelUpdateFail: "Secret のラベル更新に失敗: %s",
	MsgGCPSecretVersionAddFail:  "Secret バージョンの追加に失敗: %s",

	// ----- Validate サブコマンド -----
	MsgValidateHeader:               "=== validate: %s ===\n",
	MsgValidateProviderUnsupported:  "  [スキップ] %s: validate 未対応\n",
	MsgValidateSourceEnv:            "環境変数",
	MsgValidateSourceConfig:         "config ファイル",
	MsgValidateSourceProjectJSON:    ".vercel/project.json",
	MsgValidateSourceGitRemote:      "git remote",
	MsgValidateSourceUnset:          "(未設定)",
	MsgValidateTokenMasked:          "[設定済み] (取得元: %s)",
	MsgValidateTokenUnset:           "[未設定]",
	MsgValidateHTTPStatus:           "HTTP %d",
	MsgValidateOK:                   "OK",
	MsgValidateTokenUnsetSkip:       "  token が未設定のため API 確認をスキップします\n",
	MsgValidateProjectIDUnsetSkip:   "  projectId が未設定のため API 確認をスキップします\n",
	MsgValidateRepoUnresolvableSkip: "  repo を解決できなかったため API 確認をスキップします\n",
	MsgValidateVercelCause404:       "  推定原因: teamId 未設定、または projectId が一致しない\n",
	MsgValidateVercelCause401:       "  推定原因: token が無効\n",
	MsgValidateVercelCause403:       "  推定原因: token のスコープが不足\n",
	MsgValidateGitHubCause404:       "  推定原因: リポジトリが存在しない、または private リポジトリへのアクセス不可\n",
	MsgValidateGitHubCause401:       "  推定原因: token が無効\n",
	MsgValidateGitHubCause403:       "  推定原因: token のスコープ不足または rate limit\n",
	MsgValidateResult:               "validate: 成功 %d / 失敗 %d\n",
	MsgValidateVercelProjectID:      "  projectId : %s (取得元: %s)\n",
	MsgValidateVercelTeamID:         "  teamId    : %s (取得元: %s)\n",
	MsgValidateGitHubRepo:           "  repo      : %s (取得元: %s)\n",

	// ----- Sync / Entry 解決 -----
	MsgDefaultsProviderInvalid: "defaults.provider: 不正な provider 値 %q（%s のいずれかを指定してください）",
	MsgDefaultsProviderEmpty:   "defaults.provider に空配列が指定されています（%s のいずれかを指定してください）",
	MsgVarProviderEmpty:        "%s: provider に空配列が指定されています（%s のいずれかを指定してください）",
	MsgVarProviderBlank:        "%s: provider の指定が空または空白のみです（%s のいずれかを指定してください）",
	MsgVarProviderInvalid:      "%s: 不正な provider 値 %q（%s のいずれかを指定してください）",
}
