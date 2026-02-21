# MADFLOW 実装計画書

## 0. 前提

- 本計画は `README.md`（MADF要件定義書）に基づく
- 対象プロジェクトのコードベースは未作成（ゼロからの実装）
- 各エージェントの実体は Claude Code のサブプロセスとして起動する

## 1. 技術スタック

| 項目 | 選定 | 理由 |
| --- | --- | --- |
| 言語 | Go 1.23+ | goroutine による並行処理、シングルバイナリ配布 |
| エージェント実行基盤 | Claude Code (サブプロセス) | `claude` コマンドをプロセスとして起動し、各エージェントにツール実行力を持たせる |
| GitHub連携 | `gh` CLI | 追加依存なし、認証も `gh auth` に委譲。GitHub Issue との同期用（オプショナル） |
| Git操作 | `git` コマンド直接実行 | 追加依存なし、`os/exec` で呼び出し |
| 設定管理 | TOML (`github.com/BurntSushi/toml`) | Go で広く使われる TOML パーサー |
| テスト | 標準 `testing` パッケージ | 外部依存不要 |

### 外部依存の方針

Go 標準ライブラリ + TOML パーサーのみを依存とし、それ以外は `git` / `gh` / `claude` の CLI ツールを `os/exec` で呼び出す構成とする。

### ロール別推奨モデル

| ロール | 推奨モデル | 選定理由 |
| --- | --- | --- |
| 監督 (Superintendent) | `claude-opus-4-6` | プロジェクト全体の判断、ボトルネック分析など高度な推論が必要 |
| PM | `claude-sonnet-4-6` | タスク差配・進捗管理は定型的だが判断力も要求される。コストとのバランス |
| アーキテクト | `claude-opus-4-6` | 設計品質がプロジェクト全体を左右する。仕様の深い理解と構造設計が必要 |
| エンジニア | `claude-sonnet-4-6` | コード実装は頻度が高くコストがかかるため、十分な実装力を持つ Sonnet で最適化 |
| レビュアー | `claude-sonnet-4-6` | コードの品質検証・仕様適合チェック。Sonnet の分析力で十分対応可能 |
| RM | `claude-haiku-4-5` | git merge 等の手続き的操作が中心。高度な推論は不要でコスト最小化を優先 |

**方針**: 判断の重要度が高いロール（監督・アーキテクト）には Opus、実行頻度が高いロール（エンジニア・レビュアー・PM）には Sonnet、手続き的なロール（RM）には Haiku を割り当て、品質とコストを両立させる。モデルは `madflow.toml` でロールごとに上書き可能とする。

## 2. ディレクトリ構成

### 2.1. ソースコード構成

```
MADFLOW/
├── README.md                     # 要件定義書（既存）
├── IMPLEMENTATION_PLAN.md        # 本文書
├── go.mod
├── go.sum
├── cmd/
│   └── madflow/
│       └── main.go               # CLI エントリーポイント
├── internal/
│   ├── config/
│   │   └── config.go             # 設定ロード・バリデーション
│   ├── project/
│   │   └── project.go            # プロジェクト検出・登録・データディレクトリ管理
│   ├── chatlog/
│   │   └── chatlog.go            # 共有チャットログの読み取り・監視
│   ├── issue/
│   │   └── issue.go              # ローカルイシュー管理
│   ├── agent/
│   │   ├── agent.go              # エージェント共通インターフェース・起動管理
│   │   ├── claude.go             # Claude Code サブプロセスの起動・通信
│   │   └── roles.go              # ロール定義・通信許可マトリクス
│   ├── orchestrator/
│   │   └── orchestrator.go       # エージェント群の起動・監視・停止
│   ├── team/
│   │   └── team.go               # 特務チームのライフサイクル管理
│   ├── git/
│   │   └── git.go                # git コマンドラッパー
│   ├── github/
│   │   └── github.go             # gh CLI ラッパー（GitHub同期用、オプショナル）
│   └── reset/
│       └── reset.go              # 8分コンテキストリセットプロトコル
├── prompts/
│   ├── superintendent.md         # 監督用システムプロンプト
│   ├── pm.md                     # PM用システムプロンプト
│   ├── architect.md              # アーキテクト用システムプロンプト
│   ├── engineer.md               # エンジニア用システムプロンプト
│   ├── reviewer.md               # レビュアー用システムプロンプト
│   └── release_manager.md        # RM用システムプロンプト
├── testdata/                     # テスト用フィクスチャ
│   └── ...
└── example/
    └── madflow.toml              # 設定ファイルのサンプル
```

### 2.2. プロジェクトデータディレクトリ (`$HOME/.madflow/`)

MADFLOW の実行時データはすべて `$HOME/.madflow/[PROJECT_ID]/` 配下に格納する。

```
$HOME/.madflow/
├── projects.toml                        # プロジェクト登録簿
└── [PROJECT_ID]/                        # プロジェクトごとのデータ
    ├── chatlog.txt                      # 共有チャットログ
    ├── state.toml                       # 稼働状態（起動中エージェント、チーム情報）
    ├── memos/                           # コンテキストリセット時の作業メモ
    │   ├── superintendent-20260221T100000.md
    │   ├── engineer-1-20260221T100800.md
    │   └── ...
    └── issues/                          # ローカルイシュー管理
        ├── 001.toml
        ├── 002.toml
        └── ...
```

#### PROJECT_ID の決定

- `madflow init` 実行時にプロジェクト名から生成（例: `my-app`）
- `$HOME/.madflow/projects.toml` にプロジェクト ID とディレクトリパスのマッピングを保持

```toml
# $HOME/.madflow/projects.toml
[projects.my-app]
paths = ["/home/user/my-app"]

[projects.my-platform]
paths = ["/home/user/platform-api", "/home/user/platform-web"]
```

### 2.3. プロジェクト検出ルール

1. カレントディレクトリに `madflow.toml` があれば、そのプロジェクトとして起動
2. カレントディレクトリが `projects.toml` のいずれかの `paths` に含まれていれば、該当プロジェクトとして起動
3. `madflow start --project my-app` のように明示指定も可能
4. いずれにも該当しない場合はエラー

## 3. アーキテクチャ概要

### 3.1. エージェントの実行モデル

各エージェントは独立した Claude Code プロセスとして起動される。

```
madflow (Go バイナリ)
  │
  ├── claude --print --system-prompt "監督プロンプト" ...    ← 監督プロセス
  ├── claude --print --system-prompt "PMプロンプト" ...      ← PMプロセス
  ├── claude --print --system-prompt "RMプロンプト" ...      ← RMプロセス
  │
  └── [特務チーム N]
       ├── claude ... ← アーキテクト
       ├── claude ... ← エンジニア
       └── claude ... ← レビュアー
```

- オーケストレーター (Go) がプロセスの起動・監視・再起動を担当
- エージェント間通信はチャットログファイルを介して行う
- 各エージェントは `git` / `gh` コマンドの実行が可能

### 3.2. チャットログによる通信

```
$HOME/.madflow/my-app/chatlog.txt
──────────────────────────────────
[2026-02-21T10:00:00] [@PM] 監督: Issue #001 がオープンされました。対応を開始してください。
[2026-02-21T10:00:15] [@アーキテクト-1] PM: Issue #001 を担当してください。チーム1を編成します。
[2026-02-21T10:01:30] [@エンジニア-1] アーキテクト-1: 設計完了。feature/issue-001 ブランチで実装を開始してください。
```

- **書き込み**: 各エージェント（Claude Code）が bash の `>>` リダイレクトで直接 append する。Linux では小さい書き込み（< 4096 bytes）の append はカーネルレベルでアトミックなため、排他制御は不要
- **読み取り・監視**: Go 側（オーケストレーター）がチャットログを監視し、新着メッセージを検知
- 各エージェントは自身宛のメンションのみを処理
- オーケストレーターがチャットログを監視し、必要に応じてチーム操作を実行

### 3.3. ローカルイシュー管理

イシューはローカルファイルとして管理し、GitHub Issue への依存をなくす。

#### Issue ID 体系

イシューの発行元によって ID のフォーマットが異なる。

| 発行元 | ID フォーマット | ファイル名の例 |
| --- | --- | --- |
| GitHub 同期 | `{owner}-{repo}-{number}` | `myorg-platform-api-042.toml` |
| CLI / 監督（手動起票） | `local-{連番}` | `local-001.toml` |

- GitHub 同期の ID は `owner/repo#number` に1対1対応するため、マルチリポジトリでも衝突しない
- チャットログでの参照例: `Issue #myorg-platform-api-042`、`Issue #local-001`
- ブランチ名: `feature/myorg-platform-api-042`、`feature/local-001`

#### イシューファイル形式

```toml
# GitHub 同期で作成された例
# $HOME/.madflow/my-platform/issues/myorg-platform-api-042.toml
id = "myorg-platform-api-042"
title = "ユーザー認証機能の実装"
url = "https://github.com/myorg/platform-api/issues/42"
status = "open"                        # open / in_progress / resolved / closed
assigned_team = 0                      # 0 = 未アサイン、1~ = チーム番号
repos = ["api"]                        # 対象リポジトリ名（空 = 全リポジトリ）
labels = ["feature"]                   # bug / feature / refactor 等

body = """
ログイン・ログアウト機能を実装する。
JWT トークンベースの認証とする。
"""

acceptance = """
- ログイン画面でメールアドレスとパスワードを入力して認証できる
- JWT トークンが発行され、以降のリクエストで認証が維持される
- ログアウトするとトークンが無効化される
"""
```

```toml
# CLI で手動作成された例
# $HOME/.madflow/my-platform/issues/local-001.toml
id = "local-001"
title = "CI パイプラインの整備"
url = ""
status = "open"
assigned_team = 0
repos = ["api", "web"]
labels = ["infra"]

body = """
GitHub Actions で CI パイプラインを構築する。
api と web の両リポジトリを対象とする。
"""

acceptance = """
- push 時にテストが自動実行される
- main ブランチへのマージにはテスト通過が必須
"""
```

#### フィールド定義

| フィールド | 必須 | 型 | 用途 |
| --- | --- | --- | --- |
| `id` | Yes | string | 一意識別子。チャットログでの参照用 (`Issue #myorg-platform-api-042`) |
| `title` | Yes | string | 何をするか |
| `url` | No | string | GitHub Issue へのリンク（同期で自動設定） |
| `status` | Yes | string | `open` / `in_progress` / `resolved` / `closed` |
| `assigned_team` | Yes | int | アサイン先チーム番号（0 = 未アサイン） |
| `repos` | No | []string | 対象リポジトリ名（マルチリポジトリ時。空=全リポジトリ） |
| `labels` | No | []string | `bug` / `feature` / `refactor` 等。PM の優先度判断に使用 |
| `body` | Yes | string | 要件の詳細。アーキテクトが設計の入力として使用 |
| `acceptance` | No | string | 完了条件。レビュアーの合否判定基準 |

#### イシューの発行元と更新タイミング

| トリガー | 誰が | 何をする | 方法 |
| --- | --- | --- | --- |
| 人間がイシューを作成 | 人間 | ファイル作成 (`status=open`) | `madflow issue create` CLI |
| 監督がボトルネックを検知 | 監督 | ファイル作成 (`status=open`) | Claude Code がファイルを直接作成 |
| GitHub 同期（自動） | オーケストレーター | 全リポジトリの Issue をインポート | 5分間隔で `gh issue list` を各リポジトリに実行 |
| 監督が新規イシューを検知 | 監督 | PM に通知 | `issues/` ディレクトリを定期的に `ls` し、前回との差分で検知 |
| PM がチームをアサイン | PM | `assigned_team` を更新 | Claude Code がファイルを直接編集 |
| チームが作業を開始 | アーキテクト | `status` → `in_progress` | Claude Code がファイルを直接編集 |
| develop へマージ完了 | RM | `status` → `resolved` | Claude Code がファイルを直接編集 |
| 人間が確認完了 | 人間 | `status` → `closed` | `madflow issue close {ID}` CLI |

#### ステータス遷移

```
open → in_progress → resolved → closed
                  ↘ open (差し戻し)
```

- `open`: 起票直後。監督がディレクトリ走査で検知し PM に通知
- `in_progress`: チームにアサインされ作業中
- `resolved`: develop ブランチへのマージ完了
- `closed`: 人間が確認し完了

### 3.4. GitHub Issue の定期同期

`madflow.toml` に `[github]` セクションがある場合、オーケストレーターがバックグラウンドで GitHub Issue をローカルに自動取り込みする。

#### 同期の仕組み

```
[5分ごと]
  オーケストレーター
    → 設定内の各リポジトリに対して:
      gh issue list -R {owner}/{repo} --state open --json number,title,url,body,labels
    → ローカルの issues/ と比較
    → 新規 Issue があればローカルファイルを作成 (status=open)
    → 既存 Issue のタイトル・本文が変わっていれば更新
```

- **方向**: GitHub → ローカルの片方向のみ。ローカルのステータス変更は GitHub に反映しない
- **間隔**: 5分（`madflow.toml` の `github.sync_interval_minutes` で変更可能）
- **ID マッピング**: `{owner}/{repo}#42` → `{owner}-{repo}-042.toml`
- **repos の自動設定**: 同期元リポジトリ名を `repos` フィールドに自動設定（例: `repos = ["api"]`）
- **フィールドマッピング**:

| GitHub | ローカル |
| --- | --- |
| `owner`, `repo`, `number` | `id` (`{owner}-{repo}-{number}`) |
| `title` | `title` |
| `url` | `url` |
| `body` | `body` |
| `labels[].name` | `labels` |
| リポジトリ名 | `repos` |
| — | `status` = `open` (初回作成時) |
| — | `acceptance` (GitHub 側に記載があれば本文から抽出を試みる) |

- ローカルで既に `in_progress` 以降のステータスに進んでいるイシューは、GitHub 側の変更で上書きしない
- `madflow sync` で手動実行も可能

### 3.5. マルチリポジトリ対応

1つのプロジェクトが複数のリポジトリを管理対象にできる。

```toml
# madflow.toml
[project]
name = "my-platform"

[[project.repos]]
name = "api"
path = "/home/user/platform-api"

[[project.repos]]
name = "web"
path = "/home/user/platform-web"
```

- イシューにはどのリポジトリが対象かを指定できる（`repos = ["api"]` など）
- 各リポジトリで独立したブランチ戦略を適用
- 特務チームのエンジニアは、アサインされたリポジトリの worktree で作業

### 3.6. ブランチ運用ポリシー

```
feature/issue-{ID}  ──[レビュー通過]──→  develop  ──[人間の指示]──→  main
                     自動（RM判断）                  madflow release
```

| マージ | 許可条件 | 人間の承認 |
| --- | --- | --- |
| `feature → develop` | レビュアーが承認済み | **不要** — RM が即座にマージ |
| `develop → main` | 人間が `madflow release` を実行 | **必須** — 人間の明示的な指示がない限り禁止 |

- RM は `feature → develop` に関して自律的に行動する。レビュー通過の通知を受けたら待機せずマージする
- `develop → main` は人間のみが起動できる。監督・RM を含むすべてのエージェントに自律的なリリースを禁止する
- `madflow release` 実行時、RM が `develop → main` のマージを実施し、完了を報告する

## 4. フェーズ別実装計画

### Phase 1: 基盤レイヤー

最小限の骨格を構築し、エージェント1体が動作するところまで確認する。

#### 1-1. プロジェクト初期化

- `go.mod` の作成（モジュール定義）
- `.gitignore` 作成
- ディレクトリ構造の作成

#### 1-2. 設定管理 (`internal/config/`)

- `madflow.toml` のロードとバリデーション
- 設定項目: プロジェクト名、リポジトリパス（複数対応）、コンテキストリセット間隔（デフォルト8分）

```go
type Config struct {
    Project  ProjectConfig
    Agent    AgentConfig
    Branches BranchConfig
    GitHub   *GitHubConfig  // オプショナル
}

type ProjectConfig struct {
    Name  string
    Repos []RepoConfig
}

type RepoConfig struct {
    Name string
    Path string
}
```

#### 1-3. プロジェクト管理 (`internal/project/`)

- `$HOME/.madflow/` ディレクトリの初期化
- `projects.toml` への登録・検索
- カレントディレクトリからのプロジェクト自動検出
- プロジェクトデータディレクトリ (`$HOME/.madflow/[PROJECT_ID]/`) の作成

```go
func Detect() (*Project, error)                  // カレントディレクトリから検出
func Init(name string, paths []string) error     // 新規プロジェクト登録
func DataDir(projectID string) string            // データディレクトリパスを返す
```

#### 1-4. データモデル (`internal/agent/roles.go`, `internal/chatlog/`, `internal/issue/`)

| モデル | 用途 |
| --- | --- |
| `ChatMessage` | チャットログ1行分（宛先、発信者、本文、タイムスタンプ） |
| `WorkMemo` | 作業メモ（状態、決定事項、未解決課題、次の一手） |
| `Role` | エージェントのロール種別（enum 相当） |
| `AgentID` | エージェントの一意識別子（ロール + チーム番号） |
| `Issue` | イシュー（ID、タイトル、本文、ステータス、完了条件、ラベル等） |

#### 1-5. 共有チャットログ (`internal/chatlog/`)

- **書き込みは Go 側では行わない** — エージェント（Claude Code）が `echo "..." >> chatlog.txt` で直接書き込む
- メンション付きメッセージの parse
- 特定宛先のメッセージをフィルタする `Poll()` 関数
- ファイル末尾を goroutine で監視し、新着メッセージを channel に流す `Watch()`

```go
type ChatLog struct {
    path string  // $HOME/.madflow/[PROJECT_ID]/chatlog.txt
}

func (c *ChatLog) Poll(recipient AgentID) ([]ChatMessage, error)
func (c *ChatLog) Watch(ctx context.Context, recipient AgentID) <-chan ChatMessage
func ParseMessage(line string) (ChatMessage, error)
```

#### 1-6. ローカルイシュー管理 (`internal/issue/`)

- イシューファイル (TOML) の CRUD
- 連番 ID の自動採番
- ステータス遷移のバリデーション
- イシューファイルの読み書きは Go 側 (CLI) とエージェント (Claude Code) の両方から行われる

```go
type Issue struct {
    ID           string
    Title        string
    URL          string
    Status       string   // open / in_progress / resolved / closed
    AssignedTeam int
    Repos        []string
    Labels       []string
    Body         string
    Acceptance   string
}

type Store struct {
    dir string  // $HOME/.madflow/[PROJECT_ID]/issues/
}

func (s *Store) Create(title, body string) (*Issue, error)
func (s *Store) Get(id string) (*Issue, error)
func (s *Store) List(filter StatusFilter) ([]*Issue, error)
func (s *Store) ListNew(known []string) ([]*Issue, error)  // 既知ID一覧との差分で新規検知
func (s *Store) Update(issue *Issue) error
```

---

### Phase 2: エージェント基盤

#### 2-1. Claude Code サブプロセス管理 (`internal/agent/claude.go`)

- `claude` コマンドのサブプロセス起動・stdin/stdout 通信
- システムプロンプトの注入（`prompts/*.md` をロード）
- プロセスの生存監視と再起動

```go
type ClaudeProcess struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout io.ReadCloser
}

func StartClaude(ctx context.Context, systemPrompt string) (*ClaudeProcess, error)
func (c *ClaudeProcess) Send(input string) (string, error)
func (c *ClaudeProcess) Kill() error
```

#### 2-2. エージェント共通インターフェース (`internal/agent/agent.go`)

```go
type Agent struct {
    ID              AgentID
    Role            Role
    AllowedTargets  []Role          // 通信可能な宛先ロール（鎖の原則）
    Process         *ClaudeProcess
    ChatLog         *chatlog.ChatLog
    IssueStore      *issue.Store
    ResetInterval   time.Duration
}

func (a *Agent) Run(ctx context.Context) error  // メインループ
func (a *Agent) Reset() error                   // コンテキストリセット
```

- メインループ: チャットログ監視 → Claude に判断を問い合わせ → アクション実行 → チャットログに応答
- `AllowedTargets` による送信先バリデーション（鎖の原則の強制）

#### 2-3. コンテキストリセット (`internal/reset/`)

- エージェント起動からの経過時間を `time.Timer` で計測
- 8分到達時に Claude プロセスへ作業メモの蒸留を指示
- 作業メモを `$HOME/.madflow/[PROJECT_ID]/memos/` に保存
- プロセスを終了し、「ロールプロンプト + 元の依頼 + 直近の作業メモ」で新プロセスを起動
- `Agent.Run()` ループに組み込む

---

### Phase 3: 各ロールのプロンプト設計

Claude Code をサブプロセスとして利用するため、各ロールの振る舞いは**システムプロンプト**で制御する。Go コード側ではロール固有のロジックは最小限とし、プロンプトに委譲する。

#### 3-1. 監督プロンプト (`prompts/superintendent.md`)

指示内容:
- `$HOME/.madflow/[PROJECT_ID]/issues/` を監視し、`open` ステータスの新規イシューを検知
- 新規イシューを PM へチャットログで通知
- PM からのエスカレーションに対して判断を下す
- チャットログを定期分析し、ボトルネックを検知したらイシューファイルを作成して起票
- 人間への確認が必要な場合はイシューにコメントを追加

#### 3-2. PM プロンプト (`prompts/pm.md`)

指示内容:
- 監督からのイシュー通知を受けてチーム編成を要求（チャットログに `[@orchestrator]` メッセージ）
- 各チームの進捗をチャットログから追跡
- 遅延やブロッカーを検知したらアーキテクトに確認

#### 3-3. アーキテクト プロンプト (`prompts/architect.md`)

指示内容:
- PM からのアサインを受けて設計を開始
- `git checkout -b feature/issue-{ID}` でブランチ作成
- イシューファイルにコメントとして設計仕様を記述
- エンジニアからの質問に回答

#### 3-4. エンジニア プロンプト (`prompts/engineer.md`)

指示内容:
- アーキテクトの設計に基づきコードを実装
- `feature` ブランチ上で `git add` / `git commit`
- 実装完了後、レビュアーにレビュー依頼を送信

#### 3-5. レビュアー プロンプト (`prompts/reviewer.md`)

指示内容:
- エンジニアからのレビュー依頼を受けてコードを検証 (`git diff`)
- OK → RM にマージ依頼
- NG → 具体的な指摘と共にエンジニアへ差し戻し

#### 3-6. RM プロンプト (`prompts/release_manager.md`)

指示内容:
- **`feature → develop`**: レビュアーの承認を受けたら**人間の許可なく即座にマージ**する。待機は不要
- マージ後、イシューのステータスを `resolved` に更新し、チーム解散を通知
- **`develop → main`**: **人間から `madflow release` コマンドで明示的に指示があるまで絶対に実行しない**。自律判断でのリリースは禁止

---

### Phase 4: オーケストレーション

#### 4-1. オーケストレーター (`internal/orchestrator/`)

- 全エージェントのライフサイクル管理
  - 常駐エージェント（監督、PM、RM）の起動・維持
  - 特務チームの動的生成・解散
- goroutine + `context.Context` による並行実行とキャンセル
- エージェント異常終了時の自動再起動
- チャットログ上の `[@orchestrator]` メッセージを監視し、チーム操作を実行

#### 4-2. 特務チーム管理 (`internal/team/`)

- チーム生成: アーキテクト + エンジニア + レビュアーの3プロセスを起動
- チーム解散: 3プロセスを停止、リソースを解放
- チーム一覧・状態の管理

#### 4-3. Git 操作 (`internal/git/`)

- `os/exec` で `git` コマンドを実行するラッパー
- ブランチの作成 / マージ / 削除
- コンフリクト検知（`git merge` の exit code で判定）
- マルチリポジトリ対応: 操作対象リポジトリのパスを引数で受け取る

#### 4-4. GitHub Issue 定期同期 (`internal/github/`) — オプショナル

`madflow.toml` に `[github]` セクションがある場合のみ有効。

- オーケストレーター内で goroutine として5分間隔で実行
- `gh issue list --state open --json number,title,url,body,labels` で取得
- ローカル `issues/` との差分を比較し、新規 Issue のファイル作成・既存の更新を行う
- ローカルで `in_progress` 以降に進んでいるイシューは GitHub 側の変更で上書きしない
- `madflow sync` コマンドで手動実行も可能

```go
type Syncer struct {
    store    *issue.Store
    owner    string
    repos    []string
    interval time.Duration
}

func (s *Syncer) Run(ctx context.Context) error   // 定期同期ループ
func (s *Syncer) SyncOnce() error                 // 1回分の同期処理
```

---

### Phase 5: CLI

#### 5-1. CLI コマンド体系 (`cmd/madflow/main.go`)

Go 標準の `flag` パッケージまたはサブコマンドパターンで実装。

```
madflow init                        # カレントディレクトリをプロジェクトとして登録、madflow.toml 生成
madflow init --name my-platform \
  --repo /path/to/api \
  --repo /path/to/web              # マルチリポジトリプロジェクトの初期化
madflow start                       # 全エージェントを起動し、チャットログを stdout にリアルタイム表示
madflow start -d                    # デーモンモードで起動（チャットログ表示なし）
madflow start --project my-app      # プロジェクトを明示指定して起動
madflow status                      # 稼働中エージェント・チームの状態表示
madflow logs                        # デーモン起動中のチャットログをリアルタイム表示（tail -f 相当）
madflow logs -n 50                  # 直近50行を表示してからリアルタイム表示を開始
madflow stop                        # 全エージェントを停止
madflow issue create "タイトル"      # イシューを手動作成
madflow issue list                  # イシュー一覧表示
madflow issue show 001              # イシュー詳細表示
madflow issue close 001             # イシューをクローズ
madflow release                     # develop → main のマージを RM に指示（人間のみ実行可能）
madflow sync                        # GitHub Issue の取り込み（オプショナル）
```

#### 5-2. チャットログのリアルタイム表示

`internal/chatlog/` の `Watch()` を利用し、`tail -f` のようにチャットログを stdout へストリーム表示する。

- **`madflow start`** (フォアグラウンド): エージェント起動と同時に自動でストリーム表示開始。`Ctrl+C` で全エージェント停止
- **`madflow logs`**: デーモン起動中のチャットログに接続して表示。`-n` オプションで過去ログの遡り行数を指定可能（デフォルト: 20行）
- 表示時にロールごとの色分けを行い視認性を確保（例: 監督=赤、PM=青、アーキテクト=緑、エンジニア=黄、レビュアー=紫、RM=シアン）

---

### Phase 6: テストと品質保証

#### 6-1. ユニットテスト

| 対象 | テスト内容 |
| --- | --- |
| `internal/chatlog/` | メッセージの parse、フィルタリング、Watch の動作 |
| `internal/issue/` | イシューの CRUD、ステータス遷移、コメント追加 |
| `internal/project/` | プロジェクト検出、登録、データディレクトリ管理 |
| `internal/reset/` | タイマー発火、作業メモの蒸留指示 |
| `internal/config/` | 設定ファイルのロード・デフォルト値・バリデーション |
| `internal/agent/roles.go` | 通信許可マトリクスの検証 |
| `internal/git/` | git コマンドラッパーの動作（テスト用リポジトリ使用） |

#### 6-2. 統合テスト

- エージェント2体間のチャットログ通信
- イシュー起票からチームアサイン、マージ、クローズまでの一連フロー（Claude プロセスのモック使用）
- コンテキストリセット後の作業継続
- マルチリポジトリ構成でのブランチ操作
- GitHub Issue 同期: 新規取り込み、既存更新、in_progress 以降のスキップ確認

---

## 5. 実装優先順

```
Phase 1 (基盤)
  ├── 1-1 プロジェクト初期化
  ├── 1-2 設定管理
  ├── 1-3 プロジェクト管理
  ├── 1-4 データモデル
  ├── 1-5 チャットログ
  └── 1-6 ローカルイシュー管理
      ↓
Phase 2 (エージェント基盤)
  ├── 2-1 Claude Code サブプロセス管理
  ├── 2-2 エージェント共通インターフェース
  └── 2-3 コンテキストリセット
      ↓
Phase 3 (各ロールのプロンプト設計) ← 並行作業可能
  ├── 3-1 監督
  ├── 3-2 PM
  ├── 3-3 アーキテクト
  ├── 3-4 エンジニア
  ├── 3-5 レビュアー
  └── 3-6 RM
      ↓
Phase 4 (オーケストレーション)
  ├── 4-1 オーケストレーター
  ├── 4-2 特務チーム管理
  ├── 4-3 Git操作
  └── 4-4 GitHub同期（オプショナル）
      ↓
Phase 5 (CLI)
      ↓
Phase 6 (テスト・品質保証)
```

## 6. 設定ファイル例 (`madflow.toml`)

### シングルリポジトリ

```toml
[project]
name = "my-app"

[[project.repos]]
name = "main"
path = "."

[agent]
context_reset_minutes = 8

[agent.models]
superintendent = "claude-opus-4-6"
pm = "claude-sonnet-4-6"
architect = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"
reviewer = "claude-sonnet-4-6"
release_manager = "claude-haiku-4-5"

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
```

### マルチリポジトリ

```toml
[project]
name = "my-platform"

[[project.repos]]
name = "api"
path = "/home/user/platform-api"

[[project.repos]]
name = "web"
path = "/home/user/platform-web"

[agent]
context_reset_minutes = 8

[agent.models]
superintendent = "claude-opus-4-6"
pm = "claude-sonnet-4-6"
architect = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"
reviewer = "claude-sonnet-4-6"
release_manager = "claude-haiku-4-5"

# GitHub Issue 定期同期を使う場合（オプショナル）
[github]
owner = "myorg"
repos = ["platform-api", "platform-web"]
sync_interval_minutes = 5

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
```

## 7. リスクと対策

| リスク | 影響 | 対策 |
| --- | --- | --- |
| Claude Code プロセスの予期しない終了 | エージェント停止 | オーケストレーターによる自動再起動、作業メモからの復旧 |
| チャットログの競合書き込み | メッセージ欠損 | `>>` append はカーネルレベルでアトミック（< 4096 bytes）。メッセージサイズ超過時は分割書き込みで対応 |
| コンテキストリセット時の情報欠落 | 作業の後退 | 作業メモのフォーマットを厳格化、リセット前後のテスト |
| Git コンフリクト | マージ失敗 | コンフリクト検知 → アーキテクトへエスカレーション |
| エージェントの暴走 | 意図しない変更 | Claude Code の許可モード活用、アクションのバリデーション |
| 複数 Claude プロセスの API コスト | コスト増大 | 同時稼働チーム数の上限設定、PM によるスケーリング制御 |
| イシューファイルの同時書き込み | データ破損 | イシュー書き込みは監督・RM 等の限定ロールのみ。Go 側の Store 経由で排他制御 |
| マルチリポジトリ間の整合性 | 不整合な状態 | イシュー単位で対象リポジトリを明示、チーム内で作業リポジトリを限定 |
