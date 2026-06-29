// Package i18n はCLIメッセージの国際化（i18n）基盤を提供する。
// 他の internal パッケージへの依存は持たない（循環 import 防止）。
package i18n

// MsgKey はメッセージカタログのキー型。
type MsgKey string

// メッセージキー定数。命名規則: Msg<カテゴリ><内容>
const (
	// ----- CLI フラグ共通 -----

	// MsgFlagNeedsValue はフラグに値が必要なときのエラー（書式: フラグ名）。
	MsgFlagNeedsValue MsgKey = "flag.needs_value"
	// MsgFlagNeedsNonEmpty はフラグに空でない値が必要なときのエラー（書式: フラグ名）。
	MsgFlagNeedsNonEmpty MsgKey = "flag.needs_non_empty"
	// MsgFlagUnknown は不明な引数が指定されたときのエラー（書式: 引数名）。
	MsgFlagUnknown MsgKey = "flag.unknown"
	// MsgFlagProviderInvalid は --provider に不正な値が指定されたときのエラー（書式: 候補一覧）。
	MsgFlagProviderInvalid MsgKey = "flag.provider_invalid"
	// MsgFlagOrSeparator はプロバイダ名一覧を結合するセパレータ。
	MsgFlagOrSeparator MsgKey = "flag.or_separator"

	// ----- Main / run() -----

	// MsgErrorPrefix はエラーの標準エラー出力プレフィックス（書式: エラー文字列）。
	MsgErrorPrefix MsgKey = "error.prefix"
	// MsgEnvFileNotFound は env ファイルが見つからないエラー（書式: ファイルパス）。
	MsgEnvFileNotFound MsgKey = "env.file_not_found"
	// MsgDefFileNotFound は定義ファイルが見つからないエラー（書式: ファイルパス）。
	MsgDefFileNotFound MsgKey = "def.file_not_found"
	// MsgEnvFileReadFail は env ファイルの読み込み失敗（書式: エラー）。
	MsgEnvFileReadFail MsgKey = "env.file_read_fail"
	// MsgDefFileReadFail は定義ファイルの読み込み失敗（書式: エラー）。
	MsgDefFileReadFail MsgKey = "def.file_read_fail"
	// MsgDefFileYAMLFail は定義ファイルの YAML パース失敗（書式: エラー）。
	MsgDefFileYAMLFail MsgKey = "def.file_yaml_fail"
	// MsgSkipNoValueInEnv は定義にあるが env に値がないキーのスキップ警告（書式: キー名, envファイル名）。
	MsgSkipNoValueInEnv MsgKey = "skip.no_value_in_env"
	// MsgSkipNotDefined は env にあるが定義にないキーのスキップ警告（書式: キー名, envファイル名）。
	MsgSkipNotDefined MsgKey = "skip.not_defined"
	// MsgUsage は --help で表示する全体使用方法テキスト（書式引数なし）。
	MsgUsage MsgKey = "usage"

	// ----- 共通同期メッセージ -----

	// MsgNoEntries は登録対象がないときのメッセージ。
	MsgNoEntries MsgKey = "sync.no_entries"
	// MsgDryRun は dry-run 時の送信しませんメッセージ。
	MsgDryRun MsgKey = "sync.dry_run"
	// MsgNonInteractiveErr は非対話環境での確認エラー。
	MsgNonInteractiveErr MsgKey = "sync.non_interactive"
	// MsgAborted はユーザが中止を選択したときのメッセージ。
	MsgAborted MsgKey = "sync.aborted"
	// MsgCompleted は同期完了メッセージ（書式: 成功数, 失敗数）。
	MsgCompleted MsgKey = "sync.completed"
	// MsgTotalCompleted は複数ターゲット完了メッセージ（書式: 成功数, 失敗数）。
	MsgTotalCompleted MsgKey = "sync.total_completed"
	// MsgEntriesClassified は新規/更新件数付き登録対象表示（書式: 件数, 新規数, 更新数）。
	MsgEntriesClassified MsgKey = "sync.entries_classified"
	// MsgEntriesCount は登録対象件数（書式: 件数）。
	MsgEntriesCount MsgKey = "sync.entries_count"
	// MsgLabelUpdate は更新ラベル。
	MsgLabelUpdate MsgKey = "sync.label_update"
	// MsgLabelNew は新規ラベル。
	MsgLabelNew MsgKey = "sync.label_new"
	// MsgRequestCreateFail はリクエスト生成失敗の短いラベル（%w ラップ用）。
	MsgRequestCreateFail MsgKey = "sync.request_create_fail"
	// MsgSendFail は送信失敗の短いラベル（%w ラップ用）。
	MsgSendFail MsgKey = "sync.send_fail"
	// MsgRequestFail はリクエスト失敗の短いラベル（%w ラップ用）。
	MsgRequestFail MsgKey = "sync.request_fail"

	// ----- Config: ProviderVal -----

	// MsgProviderStringOrArray は provider に不正な YAML 型が指定されたときのエラー。
	MsgProviderStringOrArray MsgKey = "config.provider_string_or_array"

	// ----- Config: AppConfig -----

	// MsgConfigReadFail は config ファイルの読み込み失敗（書式: パス）。%w は呼び出し側で付ける。
	MsgConfigReadFail MsgKey = "config.read_fail"
	// MsgConfigYAMLFail は config ファイルの YAML パース失敗（書式: パス）。%w は呼び出し側で付ける。
	MsgConfigYAMLFail MsgKey = "config.yaml_fail"
	// MsgConfigEnvRefUnset は config 内の環境変数参照が未設定のエラー（書式: 変数名リスト）。
	MsgConfigEnvRefUnset MsgKey = "config.env_ref_unset"
	// MsgConfigEmptyVarName は config 内で空の変数名が使われたときのラベル。
	MsgConfigEmptyVarName MsgKey = "config.empty_var_name"
	// MsgConfigPermWarning は config ファイルのパーミッション警告（書式: パス, パーミッション, パス）。
	MsgConfigPermWarning MsgKey = "config.perm_warning"
	// MsgVercelProjectsNotDefined は vercel.projects 未定義で --vercel-project 指定時のエラー。
	MsgVercelProjectsNotDefined MsgKey = "config.vercel_projects_undef"
	// MsgVercelProjectNameNotFound は指定プロジェクト名が config に無いエラー（書式: プロジェクト名）。
	MsgVercelProjectNameNotFound MsgKey = "config.vercel_project_not_found"
	// MsgVercelProjectNameRequired は vercel.projects の name 必須エラー。
	MsgVercelProjectNameRequired MsgKey = "config.vercel_project_name_req"
	// MsgVercelProjectNameDuplicate は vercel.projects の name 重複エラー（書式: 名前）。
	MsgVercelProjectNameDuplicate MsgKey = "config.vercel_project_name_dup"
	// MsgVercelProjectIDRequired は vercel.projects の project_id 必須エラー（書式: 名前）。
	MsgVercelProjectIDRequired MsgKey = "config.vercel_project_id_req"
	// MsgGitHubReposNotDefined は github.repos 未定義で --github-repo 指定時のエラー。
	MsgGitHubReposNotDefined MsgKey = "config.github_repos_undef"
	// MsgGitHubRepoNameNotFound は指定リポジトリ名が config に無いエラー（書式: リポジトリ名）。
	MsgGitHubRepoNameNotFound MsgKey = "config.github_repo_not_found"
	// MsgGitHubRepoNameRequired は github.repos の name 必須エラー。
	MsgGitHubRepoNameRequired MsgKey = "config.github_repo_name_req"
	// MsgGitHubRepoNameDuplicate は github.repos の name 重複エラー（書式: 名前）。
	MsgGitHubRepoNameDuplicate MsgKey = "config.github_repo_name_dup"
	// MsgGitHubRepoRepoRequired は github.repos の repo 必須エラー（書式: 名前）。
	MsgGitHubRepoRepoRequired MsgKey = "config.github_repo_repo_req"

	// ----- Init サブコマンド -----

	// MsgInitEnvFileReadFail は init での env ファイル読み込み失敗（書式: パス, エラー）。
	MsgInitEnvFileReadFail MsgKey = "init.env_file_read_fail"
	// MsgFileExists はファイルが既に存在するエラー（書式: パス）。
	MsgFileExists MsgKey = "init.file_exists"
	// MsgInitDefWriteFail は定義ファイル書き込み失敗（書式: パス, エラー）。
	MsgInitDefWriteFail MsgKey = "init.def_write_fail"
	// MsgGenerated は生成完了メッセージ（書式: パス）。
	MsgGenerated MsgKey = "init.generated"
	// MsgInitKeyCount はキー数表示（書式: 件数）。
	MsgInitKeyCount MsgKey = "init.key_count"
	// MsgInitKeyListHeader はキー一覧ヘッダ。
	MsgInitKeyListHeader MsgKey = "init.key_list_header"
	// MsgInitSecretNote は secret 見直し注記。
	MsgInitSecretNote MsgKey = "init.secret_note"
	// MsgInitYAMLHeader は生成 YAML のコメントヘッダブロック（書式引数なし）。
	MsgInitYAMLHeader MsgKey = "init.yaml_header"
	// MsgInitYAMLExample は生成 YAML の例コメントブロック（書式引数なし）。
	MsgInitYAMLExample MsgKey = "init.yaml_example"

	// ----- Setup サブコマンド -----

	// MsgSetupNonInteractive は非 TTY 環境での setup エラー（書式引数なし）。
	MsgSetupNonInteractive MsgKey = "setup.non_interactive"
	// MsgSetupHomeDirFail はホームディレクトリ取得失敗。
	MsgSetupHomeDirFail MsgKey = "setup.home_dir_fail"
	// MsgSetupDirCreateFail はディレクトリ作成失敗（書式: パス, エラー）。
	MsgSetupDirCreateFail MsgKey = "setup.dir_create_fail"
	// MsgSetupConfigWriteFail は config ファイル書き込み失敗（書式: パス, エラー）。
	MsgSetupConfigWriteFail MsgKey = "setup.config_write_fail"
	// MsgSetupConfigChmodFail は config ファイルのパーミッション設定失敗（書式: パス, エラー）。
	MsgSetupConfigChmodFail MsgKey = "setup.config_chmod_fail"
	// MsgSetupInputReadFail は対話入力読み込み失敗のラベル（%w は呼び出し側で付ける）。
	MsgSetupInputReadFail MsgKey = "setup.input_read_fail"
	// MsgSetupTokenNote はトークン含有時の注記。
	MsgSetupTokenNote MsgKey = "setup.token_note"
	// MsgSetupGitignoreNote は .gitignore 追記推奨の注記。
	MsgSetupGitignoreNote MsgKey = "setup.gitignore_note"
	// MsgSetupAskVercel は Vercel 設定を使うか確認プロンプト。
	MsgSetupAskVercel MsgKey = "setup.ask_vercel"
	// MsgSetupVercelProjectID は Vercel project_id 入力プロンプト。
	MsgSetupVercelProjectID MsgKey = "setup.vercel_project_id"
	// MsgSetupVercelTeamID は Vercel team_id 入力プロンプト。
	MsgSetupVercelTeamID MsgKey = "setup.vercel_team_id"
	// MsgSetupVercelTokenEnvRef は Vercel token を環境変数参照にするか確認プロンプト。
	MsgSetupVercelTokenEnvRef MsgKey = "setup.vercel_token_env_ref"
	// MsgSetupVercelTokenPlain は Vercel token を平文で入力するプロンプト。
	MsgSetupVercelTokenPlain MsgKey = "setup.vercel_token_plain"
	// MsgSetupAskGitHub は GitHub 設定を使うか確認プロンプト。
	MsgSetupAskGitHub MsgKey = "setup.ask_github"
	// MsgSetupGitHubRepo は GitHub repo 入力プロンプト。
	MsgSetupGitHubRepo MsgKey = "setup.github_repo"
	// MsgSetupGitHubTokenEnvRef は GitHub token を環境変数参照にするか確認プロンプト。
	MsgSetupGitHubTokenEnvRef MsgKey = "setup.github_token_env_ref"
	// MsgSetupGitHubTokenPlain は GitHub token を平文で入力するプロンプト。
	MsgSetupGitHubTokenPlain MsgKey = "setup.github_token_plain"
	// MsgSetupYAMLHeader は BuildSetupYAML が生成するコメントヘッダブロック。
	MsgSetupYAMLHeader MsgKey = "setup.yaml_header"

	// ----- Vercel Provider -----

	// MsgVercelTokenMissing は VERCEL_TOKEN 未設定エラー（単一ターゲット時）。
	MsgVercelTokenMissing MsgKey = "vercel.token_missing"
	// MsgVercelTokenMissingProject は per-project VERCEL_TOKEN 未設定エラー（書式: プロジェクト名）。
	MsgVercelTokenMissingProject MsgKey = "vercel.token_missing_project"
	// MsgVercelTokenSkipProject は複数ターゲット時のトークン未設定スキップ警告（書式: プロジェクト名）。
	MsgVercelTokenSkipProject MsgKey = "vercel.token_skip_project"
	// MsgVercelExistingKeysFetchWarn は既存 key 取得失敗の警告（書式: エラー）。
	MsgVercelExistingKeysFetchWarn MsgKey = "vercel.existing_keys_warn"
	// MsgVercelTargetProject は同期先プロジェクト表示（書式: ラベル, envファイル, defファイル）。
	MsgVercelTargetProject MsgKey = "vercel.target_project"
	// MsgVercelEntriesUpsert は upsert モード登録対象件数（書式: 件数）。
	MsgVercelEntriesUpsert MsgKey = "vercel.entries_upsert"
	// MsgVercelConfirmMulti は複数プロジェクト送信確認プロンプト（書式: 件数）。
	MsgVercelConfirmMulti MsgKey = "vercel.confirm_multi"
	// MsgVercelConfirmSingle は単一プロジェクト送信確認プロンプト。
	MsgVercelConfirmSingle MsgKey = "vercel.confirm_single"
	// MsgVercelProjectSeparator は複数ターゲット時のプロジェクトセパレータ（書式: プロジェクト名）。
	MsgVercelProjectSeparator MsgKey = "vercel.project_separator"
	// MsgVercelURLBuildFailOut は URL 組み立て失敗の stderr 出力（書式: エラー）。
	MsgVercelURLBuildFailOut MsgKey = "vercel.url_build_fail_out"
	// MsgVercelRequestCreateFailOut は Vercel リクエスト生成失敗の stdout 出力（書式: キー名, エラー）。
	MsgVercelRequestCreateFailOut MsgKey = "vercel.request_create_fail_out"
	// MsgVercelSendFailOut は Vercel 送信失敗の stdout 出力（書式: キー名, エラー）。
	MsgVercelSendFailOut MsgKey = "vercel.send_fail_out"
	// MsgVercelProjectJSONReadFail は .vercel/project.json 読み込み失敗（書式: エラー）。
	MsgVercelProjectJSONReadFail MsgKey = "vercel.project_json_read_fail"
	// MsgVercelProjectJSONParseFail は .vercel/project.json パース失敗（書式: エラー）。
	MsgVercelProjectJSONParseFail MsgKey = "vercel.project_json_parse_fail"
	// MsgVercelProjectIDMissing は VERCEL_PROJECT_ID 未設定エラー。
	MsgVercelProjectIDMissing MsgKey = "vercel.project_id_missing"
	// MsgVercelProjectNotDefined は vercel_project 指定だが projects 未定義のエラー（書式: キー名）。
	MsgVercelProjectNotDefined MsgKey = "vercel.project_not_defined"
	// MsgVercelProjectInvalidConfig は vercel_project が config に存在しないエラー（書式: キー名, プロジェクト名, 定義済みリスト）。
	MsgVercelProjectInvalidConfig MsgKey = "vercel.project_invalid_config"
	// MsgVercelInvalidEnvironment は不正な environment 値エラー（書式: キー名, 値）。
	MsgVercelInvalidEnvironment MsgKey = "vercel.invalid_environment"
	// MsgVercelURLBuildFailInternal は vercelFetchExistingKeys の URL 組み立て失敗ラベル（%w ラップ用）。
	MsgVercelURLBuildFailInternal MsgKey = "vercel.url_build_fail_internal"
	// MsgVercelExistingKeyFetchFail は既存 key 取得失敗ラベル（%w ラップ用）。
	MsgVercelExistingKeyFetchFail MsgKey = "vercel.existing_key_fetch_fail"
	// MsgVercelExistingKeyParseFail は既存 key レスポンスのパース失敗ラベル（%w ラップ用）。
	MsgVercelExistingKeyParseFail MsgKey = "vercel.existing_key_parse_fail"

	// ----- GitHub Provider -----

	// MsgGitHubTokenMissing は GITHUB_TOKEN 未設定エラー（単一ターゲット時）。
	MsgGitHubTokenMissing MsgKey = "github.token_missing"
	// MsgGitHubTokenMissingRepo は per-repo GITHUB_TOKEN 未設定エラー（書式: リポジトリ名）。
	MsgGitHubTokenMissingRepo MsgKey = "github.token_missing_repo"
	// MsgGitHubTokenSkipRepo は複数ターゲット時のトークン未設定スキップ警告（書式: リポジトリ名）。
	MsgGitHubTokenSkipRepo MsgKey = "github.token_skip_repo"
	// MsgGitHubExistingCheckWarn は既存確認失敗の警告（書式: エラー）。
	MsgGitHubExistingCheckWarn MsgKey = "github.existing_check_warn"
	// MsgGitHubTargetRepo は同期先リポジトリ表示（書式: owner, repo）。
	MsgGitHubTargetRepo MsgKey = "github.target_repo"
	// MsgGitHubConfirmMulti は複数リポジトリ送信確認プロンプト（書式: 件数）。
	MsgGitHubConfirmMulti MsgKey = "github.confirm_multi"
	// MsgGitHubConfirmSingle は単一リポジトリ送信確認プロンプト。
	MsgGitHubConfirmSingle MsgKey = "github.confirm_single"
	// MsgGitHubRepoSeparator は複数ターゲット時のリポジトリセパレータ（書式: owner, repo）。
	MsgGitHubRepoSeparator MsgKey = "github.repo_separator"
	// MsgGitHubPublicKeyFetchFailOut は公開鍵取得失敗の stdout 出力（書式: キー名, エラー）。
	MsgGitHubPublicKeyFetchFailOut MsgKey = "github.pubkey_fetch_fail_out"
	// MsgGitHubEncryptFailOut は暗号化失敗の stdout 出力（書式: キー名, エラー）。
	MsgGitHubEncryptFailOut MsgKey = "github.encrypt_fail_out"
	// MsgGitHubExistCheckFailOut は存在確認失敗の stdout 出力（書式: キー名, envスコープ, エラー）。
	MsgGitHubExistCheckFailOut MsgKey = "github.exist_check_fail_out"
	// MsgGitHubExistCheckTaskFail は task 存在確認失敗ラベル（書式: キー名, envスコープ; %w は呼び出し側で付ける）。
	MsgGitHubExistCheckTaskFail MsgKey = "github.exist_check_task_fail"
	// MsgGitHubRepoFormatInvalid はリポジトリ形式不正エラー（書式: 値）。
	MsgGitHubRepoFormatInvalid MsgKey = "github.repo_format_invalid"
	// MsgGitHubRepoEnvInvalid は GITHUB_REPO の形式不正エラー。
	MsgGitHubRepoEnvInvalid MsgKey = "github.repo_env_invalid"
	// MsgGitHubRepoRequired は GITHUB_REPO 未設定エラー。
	MsgGitHubRepoRequired MsgKey = "github.repo_required"
	// MsgSealedBoxEncryptFail は sealed box 暗号化失敗ラベル（%w ラップ用）。
	MsgSealedBoxEncryptFail MsgKey = "github.sealed_box_encrypt_fail"
	// MsgPublicKeyFetchFail は公開鍵取得失敗ラベル（%w ラップ用）。
	MsgPublicKeyFetchFail MsgKey = "github.public_key_fetch_fail"
	// MsgPublicKeyParseFail は公開鍵レスポンスのパース失敗ラベル（%w ラップ用）。
	MsgPublicKeyParseFail MsgKey = "github.public_key_parse_fail"
	// MsgPublicKeyDecodeFail は公開鍵 base64 デコード失敗ラベル（%w ラップ用）。
	MsgPublicKeyDecodeFail MsgKey = "github.public_key_decode_fail"
	// MsgPublicKeyLengthInvalid は公開鍵長不正エラー（書式: バイト数）。
	MsgPublicKeyLengthInvalid MsgKey = "github.public_key_length_invalid"
	// MsgSecretExistCheckFail はシークレット存在確認失敗（書式: HTTPメッセージ）。
	MsgSecretExistCheckFail MsgKey = "github.secret_exist_check_fail"
	// MsgVariableExistCheckFail は変数存在確認失敗（書式: HTTPメッセージ）。
	MsgVariableExistCheckFail MsgKey = "github.variable_exist_check_fail"

	// ----- GCP Provider -----

	// MsgGCPProjectIDMissing は GCP_PROJECT_ID 未設定エラー。
	MsgGCPProjectIDMissing MsgKey = "gcp.project_id_missing"
	// MsgGCPSkipNotSecret は secret=false エントリのスキップ警告（書式: キー名）。
	MsgGCPSkipNotSecret MsgKey = "gcp.skip_not_secret"
	// MsgGCPTargetProject は GCP 同期先プロジェクト表示（書式: プロジェクト ID）。
	MsgGCPTargetProject MsgKey = "gcp.target_project"
	// MsgGCPLabelsNone は labels なしのラベル。
	MsgGCPLabelsNone MsgKey = "gcp.labels_none"
	// MsgGCPConfirm は GCP 送信確認プロンプト。
	MsgGCPConfirm MsgKey = "gcp.confirm"
	// MsgGCPClientCreateFail は Secret Manager クライアント作成失敗（書式: エラー）。
	MsgGCPClientCreateFail MsgKey = "gcp.client_create_fail"
	// MsgGCPSecretGetFail は Secret 取得失敗（書式: エラー）。
	MsgGCPSecretGetFail MsgKey = "gcp.secret_get_fail"
	// MsgGCPSecretCreateFail は Secret 作成失敗（書式: エラー）。
	MsgGCPSecretCreateFail MsgKey = "gcp.secret_create_fail"
	// MsgGCPSecretLabelUpdateFail は Secret ラベル更新失敗（書式: エラー）。
	MsgGCPSecretLabelUpdateFail MsgKey = "gcp.secret_label_update_fail"
	// MsgGCPSecretVersionAddFail は Secret バージョン追加失敗（書式: エラー）。
	MsgGCPSecretVersionAddFail MsgKey = "gcp.secret_version_add_fail"

	// ----- Validate サブコマンド -----

	// MsgValidateHeader は validate のターゲットヘッダ（書式: ターゲットラベル = name / projectId / owner-repo など）。
	MsgValidateHeader MsgKey = "validate.header"
	// MsgValidateProviderUnsupported は Validator 未実装 provider のスキップメッセージ（書式: プロバイダー名）。
	MsgValidateProviderUnsupported MsgKey = "validate.provider_unsupported"
	// MsgValidateSourceEnv は取得元が環境変数であることを示すラベル。
	MsgValidateSourceEnv MsgKey = "validate.source_env"
	// MsgValidateSourceConfig は取得元が config ファイルであることを示すラベル。
	MsgValidateSourceConfig MsgKey = "validate.source_config"
	// MsgValidateSourceProjectJSON は取得元が .vercel/project.json であることを示すラベル。
	MsgValidateSourceProjectJSON MsgKey = "validate.source_project_json"
	// MsgValidateSourceGitRemote は取得元が git remote であることを示すラベル。
	MsgValidateSourceGitRemote MsgKey = "validate.source_git_remote"
	// MsgValidateSourceUnset は値が未設定であることを示すラベル。
	MsgValidateSourceUnset MsgKey = "validate.source_unset"
	// MsgValidateTokenMasked はトークンがマスクされていることを示すラベル（書式: 取得元）。
	MsgValidateTokenMasked MsgKey = "validate.token_masked"
	// MsgValidateTokenUnset はトークン未設定のラベル。
	MsgValidateTokenUnset MsgKey = "validate.token_unset"
	// MsgValidateHTTPStatus は API 到達確認のステータス表示（書式: ステータスコード）。
	MsgValidateHTTPStatus MsgKey = "validate.http_status"
	// MsgValidateOK は到達確認成功のラベル。
	MsgValidateOK MsgKey = "validate.ok"
	// MsgValidateTokenUnsetSkip はトークン未設定のため API 確認をスキップするメッセージ。
	MsgValidateTokenUnsetSkip MsgKey = "validate.token_unset_skip"
	// MsgValidateProjectIDUnsetSkip は projectId 未設定のため API 確認をスキップするメッセージ（Vercel）。
	MsgValidateProjectIDUnsetSkip MsgKey = "validate.project_id_unset_skip"
	// MsgValidateRepoUnresolvableSkip は repo 解決失敗のため API 確認をスキップするメッセージ（GitHub）。
	MsgValidateRepoUnresolvableSkip MsgKey = "validate.repo_unresolvable_skip"
	// MsgValidateVercelCause404 は Vercel 404 の推定原因。
	MsgValidateVercelCause404 MsgKey = "validate.vercel_cause_404"
	// MsgValidateVercelCause401 は Vercel 401 の推定原因。
	MsgValidateVercelCause401 MsgKey = "validate.vercel_cause_401"
	// MsgValidateVercelCause403 は Vercel 403 の推定原因。
	MsgValidateVercelCause403 MsgKey = "validate.vercel_cause_403"
	// MsgValidateGitHubCause404 は GitHub 404 の推定原因。
	MsgValidateGitHubCause404 MsgKey = "validate.github_cause_404"
	// MsgValidateGitHubCause401 は GitHub 401 の推定原因。
	MsgValidateGitHubCause401 MsgKey = "validate.github_cause_401"
	// MsgValidateGitHubCause403 は GitHub 403 の推定原因。
	MsgValidateGitHubCause403 MsgKey = "validate.github_cause_403"
	// MsgValidateResult は検証結果サマリ（書式: 成功数, 失敗数）。
	MsgValidateResult MsgKey = "validate.result"
	// MsgValidateVercelProjectID は Vercel プロジェクト ID の表示（書式: ID, 取得元）。
	MsgValidateVercelProjectID MsgKey = "validate.vercel_project_id"
	// MsgValidateVercelTeamID は Vercel チーム ID の表示（書式: ID or 未設定ラベル, 取得元）。
	MsgValidateVercelTeamID MsgKey = "validate.vercel_team_id"
	// MsgValidateGitHubRepo は GitHub リポジトリの表示（書式: owner/repo, 取得元）。
	MsgValidateGitHubRepo MsgKey = "validate.github_repo"
	// ----- Sync / Entry 解決 -----

	// MsgDefaultsProviderInvalid は defaults.provider の不正値エラー（書式: 値, 候補一覧）。
	MsgDefaultsProviderInvalid MsgKey = "entry.defaults_provider_invalid"
	// MsgDefaultsProviderEmpty は defaults.provider 空配列エラー（書式: 候補一覧）。
	MsgDefaultsProviderEmpty MsgKey = "entry.defaults_provider_empty"
	// MsgVarProviderEmpty は変数 provider 空配列エラー（書式: キー名, 候補一覧）。
	MsgVarProviderEmpty MsgKey = "entry.var_provider_empty"
	// MsgVarProviderBlank は変数 provider 空白のみエラー（書式: キー名, 候補一覧）。
	MsgVarProviderBlank MsgKey = "entry.var_provider_blank"
	// MsgVarProviderInvalid は変数の不正 provider 値エラー（書式: キー名, 値, 候補一覧）。
	MsgVarProviderInvalid MsgKey = "entry.var_provider_invalid"
)
