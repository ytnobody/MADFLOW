# GitHubインテグレーション プラグイン化 実装案

## 1. 概要

本ドキュメントは、MADFLOWのGitHubインテグレーション機能をプラグインアーキテクチャへ移行するための実装案を記述する。

### 1.1. 目的

現在のMADFLOWはGitHubを前提としたハードコードされた統合機能を持つ。今後、以下のような異なるバックエンドへの対応を可能にするため、インテグレーション機能をプラグインとして抽象化する。

- **GitLabインテグレーション** — Merge Requestを持つプラットフォーム
- **Azure DevOpsインテグレーション** — Work ItemsとPull Requestを持つプラットフォーム
- **Google Spreadsheetによるイシュー管理** — Pull Request相当機能を持たないプラットフォーム

現在のGitHubインテグレーション機能は、**組み込みプラグイン（Built-in Plugin）** として最初から利用可能な状態を維持する。

### 1.2. スコープ

- プラグインインターフェース仕様の定義
- 組み込みGitHubプラグインの位置づけ
- Pull Request相当機能が存在しないプラットフォームへの対応方針
- 設定ファイル（`madflow.toml`）の拡張方針
- ディレクトリ構成と実装方針

---

## 2. 現状の課題

### 2.1. 現在のアーキテクチャ

```
internal/
  github/
    github.go      — イシュー同期（Syncer）
    events.go      — イベント監視（EventWatcher）
    idle.go        — アイドル検出（IdleDetector）
    ratelimit.go   — レート制限チェック
  orchestrator/
    orchestrator.go — GitHub Syncerを直接利用
```

現在の実装では以下の問題がある。

1. `gh` CLIへの直接依存 — `gh issue list`、`gh api`、`gh issue close` 等をサブプロセス呼び出しで実行
2. GitHub固有のデータ構造が内部に散在 — `ghIssue`、`ghComment`、`ghEvent` 等
3. オーケストレーターがGitHub Syncer/EventWatcherを直接生成・管理 — 別プロバイダへの切り替えが困難
4. IDフォーマットがGitHub前提 — `{owner}-{repo}-{number}` 形式

---

## 3. プラグインインターフェース設計

### 3.1. ディレクトリ構成

```
internal/
  integration/
    plugin.go          — プラグインインターフェース定義
    registry.go        — プラグイン登録・ファクトリ
    builtin/
      github/          — 組み込みGitHubプラグイン（現 internal/github/ を移行）
        provider.go
        syncer.go
        events.go
        idle.go
        ratelimit.go
```

> 現在の `internal/github/` パッケージは `internal/integration/builtin/github/` へ移動し、プラグインインターフェースを実装する形にリファクタリングする。

### 3.2. 共通データ型

プラグイン間で共通して使用するデータ型を `internal/integration/plugin.go` に定義する。

```go
package integration

import (
    "context"
    "time"

    "github.com/ytnobody/madflow/internal/issue"
)

// ProviderIssue はプロバイダから取得したイシューの共通表現
type ProviderIssue struct {
    // プロバイダ固有のID（例: GitHubなら issue number）
    ExternalID string
    // MADFLOWシステム内でのID（例: "ytnobody-MADFLOW-219"）
    SystemID   string
    Title      string
    Body       string
    State      string // "open" or "closed"
    // プロバイダのURL（GitHub Issue URL等）
    URL        string
    // イシューのラベル
    Labels     []string
    // 作成者
    Author     string
    // リポジトリ名
    Repo       string
}

// ProviderComment はプロバイダから取得したコメントの共通表現
type ProviderComment struct {
    ExternalID string
    Body       string
    Author     string
    IsBot      bool
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

// ChangeRequest はPull Request / Merge Request相当の概念の共通表現
type ChangeRequest struct {
    ExternalID string
    Title      string
    Body       string
    // 対象ブランチ（マージ先）
    BaseBranch string
    // マージ済みかどうか
    Merged     bool
    // プロバイダのURL
    URL        string
    // リポジトリ名
    Repo       string
}

// EventType はイベントの種別
type EventType int

const (
    EventTypeIssueOpened  EventType = iota
    EventTypeIssueComment
    EventTypeChangeRequestMerged
    EventTypeChangeRequestOpened
)

// Event はプロバイダから受信したイベントの共通表現
type Event struct {
    Type      EventType
    IssueID   string            // 関連するシステムISSUE ID（あれば）
    Comment   *ProviderComment  // EventTypeIssueComment の場合に設定
    ChangeReq *ChangeRequest    // EventTypeChangeRequest* の場合に設定
}

// EventCallback はイベントを受信した際に呼ばれるコールバック関数
type EventCallback func(event Event)
```

### 3.3. IssueProvider インターフェース

イシュー管理操作の抽象インターフェース。すべてのプラグインが実装しなければならない。

```go
// IssueProvider はイシュー管理バックエンドの基本インターフェース
type IssueProvider interface {
    // Name はプラグインの識別子を返す（例: "github", "gitlab"）
    Name() string

    // ListOpenIssues は指定リポジトリのオープンイシュー一覧を取得する
    ListOpenIssues(ctx context.Context, repos []string) ([]ProviderIssue, error)

    // FetchComments は指定イシューのコメント一覧を取得する
    // issueID は ExternalID（プロバイダ固有ID）を使用する
    FetchComments(ctx context.Context, repo string, issueExternalID string) ([]ProviderComment, error)

    // CloseIssue は指定イシューをクローズする
    CloseIssue(ctx context.Context, repo string, issueExternalID string) error

    // FormatSystemID はプロバイダ固有情報からMADFLOWシステムIDを生成する
    // 例 (GitHub): FormatSystemID("ytnobody", "MADFLOW", "219") → "ytnobody-MADFLOW-219"
    FormatSystemID(owner, repo, externalID string) string

    // ParseSystemID はMADFLOWシステムIDをプロバイダ固有情報に分解する
    ParseSystemID(systemID string) (owner, repo, externalID string, err error)
}
```

### 3.4. ChangeRequestProvider インターフェース（オプション）

Pull RequestやMerge Request相当の機能を持つプラットフォーム向けのオプションインターフェース。このインターフェースを実装しないプラグインは `nil` を返す。

```go
// ChangeRequestProvider はPR/MR相当の機能を持つプラットフォーム向けのインターフェース
// 実装はオプション。プラグインがこの機能をサポートしない場合は nil を返す。
type ChangeRequestProvider interface {
    // ParseChangeRequestBody はChangeRequestの本文からMADFLOW system IDを抽出する
    // 例 (GitHub): PR本文中の "Issue: ytnobody-MADFLOW-219" からIDを抽出
    ParseChangeRequestBody(body string) (systemID string, err error)
}
```

### 3.5. EventProvider インターフェース（オプション）

リアルタイムイベント通知をサポートするプラットフォーム向けのオプションインターフェース。

```go
// EventProvider はリアルタイムイベント監視をサポートするプラットフォーム向けのインターフェース
// 実装はオプション。サポートしない場合は nil を返す。
type EventProvider interface {
    // WatchEvents は指定リポジトリのイベントを監視し、イベント発生時にcallbackを呼び出す
    // ctx がキャンセルされると監視を停止する
    WatchEvents(ctx context.Context, repos []string, interval time.Duration, callback EventCallback) error
}
```

### 3.6. Plugin 構造体

3つのインターフェースをまとめるプラグイン定義。

```go
// Plugin はMADFLOWインテグレーションプラグインの定義
type Plugin struct {
    // IssueProvider は必須
    IssueProvider IssueProvider

    // ChangeRequestProvider はオプション（nilの場合はChangeRequest機能を無効化）
    ChangeRequestProvider ChangeRequestProvider

    // EventProvider はオプション（nilの場合はポーリングのみで動作）
    EventProvider EventProvider
}

// SupportsChangeRequests はこのプラグインがChangeRequest機能をサポートするか返す
func (p *Plugin) SupportsChangeRequests() bool {
    return p.ChangeRequestProvider != nil
}

// SupportsEvents はこのプラグインがリアルタイムイベント監視をサポートするか返す
func (p *Plugin) SupportsEvents() bool {
    return p.EventProvider != nil
}
```

### 3.7. プラグインレジストリ

```go
// registry.go
package integration

import "fmt"

var pluginFactories = map[string]PluginFactory{}

// PluginFactory はプラグインインスタンスを生成するファクトリ関数
type PluginFactory func(config map[string]any) (*Plugin, error)

// Register はプラグインファクトリを登録する
func Register(name string, factory PluginFactory) {
    pluginFactories[name] = factory
}

// New は指定名のプラグインを生成する
func New(name string, config map[string]any) (*Plugin, error) {
    factory, ok := pluginFactories[name]
    if !ok {
        return nil, fmt.Errorf("unknown integration plugin: %s", name)
    }
    return factory(config)
}
```

---

## 4. Pull Request相当機能が存在しないプラットフォームへの対応

### 4.1. 問題の整理

現在のMADFLOWのワークフローはPull Requestのマージイベントに強く依存している。

- PRがマージされると → イシューをクローズ → チームを解散

しかし、以下のプラットフォームにはPR相当の概念がない。

| プラットフォーム | PR相当 | 備考 |
|---|---|---|
| GitHub | Pull Request | あり |
| GitLab | Merge Request | あり（機能的には同等） |
| Azure DevOps | Pull Request | あり（Work Itemsは別途） |
| Google Spreadsheet | **なし** | スプレッドシート上のセル編集のみ |

### 4.2. 対応方針

オーケストレーターはプラグインの `SupportsChangeRequests()` を確認し、以下のように振る舞いを変更する。

#### ChangeRequest対応プラグインの場合（GitHub / GitLab / Azure DevOps）

現在と同じワークフロー：
1. エンジニアがコードを実装しPR/MRを作成
2. EventWatcherまたはポーリングでマージを検知
3. 対応イシューをクローズ → チーム解散

#### ChangeRequest非対応プラグインの場合（Google Spreadsheet等）

ChangeRequest機能がないため、代替のワークフローを採用する。

**方針A: 手動クローズ**
- オーケストレーターはPRマージによる自動クローズを行わない
- Superintendentがイシューを手動でクローズした際にチームを解散
- チャットログの `ISSUE_CLOSED {issueID}` コマンドで解散トリガー

**方針B: コミットベースクローズ（将来検討）**
- 特定のコミットメッセージパターン（`Close: {issueID}`等）をトリガーとする
- Gitフックまたは外部CIとの連携が必要

本実装案では **方針A（手動クローズ）** を採用する。

#### オーケストレーターの制御フロー

```go
// オーケストレーター内の初期化処理（疑似コード）
plugin := integration.New(cfg.Integration.Plugin, cfg.Integration.Config)

if plugin.SupportsEvents() {
    go runEventWatcher(plugin.EventProvider)
}

if plugin.SupportsChangeRequests() {
    // PRマージ自動検知フローを有効化
    enableAutoCloseOnMerge()
} else {
    // 手動クローズフローのみ有効化
    log.Info("ChangeRequest機能なし。手動クローズモードで動作します")
}
```

---

## 5. 設定ファイル（madflow.toml）の拡張

### 5.1. 現在の設定構造

```toml
[github]
  owner = "ytnobody"
  repos = ["MADFLOW"]
  sync_interval_minutes = 5
  # ...
```

### 5.2. 拡張後の設定構造

```toml
[integration]
  # 使用するプラグイン名。デフォルトは "github"（後方互換性維持）
  plugin = "github"

[integration.github]
  owner = "ytnobody"
  repos = ["MADFLOW"]
  sync_interval_minutes = 5
  event_poll_seconds = 30
  idle_poll_minutes = 60
  idle_threshold_minutes = 30
  dormancy_threshold_minutes = 240
  bot_comment_patterns = []
  authorized_users = ["ytnobody"]

[integration.gitlab]
  # GitLab固有の設定（将来実装時）
  host = "https://gitlab.com"
  group = "mygroup"
  repos = ["myproject"]
  token_env = "GITLAB_TOKEN"

[integration.azure-devops]
  # Azure DevOps固有の設定（将来実装時）
  organization = "myorg"
  project = "myproject"
  token_env = "AZURE_DEVOPS_TOKEN"

[integration.google-spreadsheet]
  # Google Spreadsheet固有の設定（将来実装時）
  spreadsheet_id = "..."
  credentials_file = "credentials.json"
```

### 5.3. 後方互換性

既存の `[github]` セクションを持つ `madflow.toml` との後方互換性を維持するため、以下のフォールバック処理を行う。

1. `[integration]` セクションが存在しない場合 → `plugin = "github"` として扱う
2. `[integration.github]` が未定義かつ `[github]` が存在する場合 → `[github]` の設定を `[integration.github]` として読み込む

---

## 6. 組み込みGitHubプラグインの実装方針

### 6.1. パッケージ移行

現在の `internal/github/` を `internal/integration/builtin/github/` へ移動し、プラグインインターフェースを実装する。

```go
// internal/integration/builtin/github/provider.go
package github

import (
    "github.com/ytnobody/madflow/internal/integration"
)

// GitHubProvider は組み込みGitHubプラグインのメイン実装
type GitHubProvider struct {
    owner  string
    repos  []string
    // 現在の Syncer が持つ設定フィールドを引き継ぐ
    authorizedUsers    []string
    botCommentPatterns []*regexp.Regexp
}

// integration.IssueProvider を実装
var _ integration.IssueProvider = (*GitHubProvider)(nil)

// integration.ChangeRequestProvider を実装
var _ integration.ChangeRequestProvider = (*GitHubProvider)(nil)

// integration.EventProvider を実装
var _ integration.EventProvider = (*GitHubProvider)(nil)

func init() {
    // 起動時に組み込みプラグインを自動登録
    integration.Register("github", func(config map[string]any) (*integration.Plugin, error) {
        p := newFromConfig(config)
        return &integration.Plugin{
            IssueProvider:         p,
            ChangeRequestProvider: p,
            EventProvider:         p,
        }, nil
    })
}
```

### 6.2. 既存機能との対応

| 現在の関数/メソッド | 対応するインターフェースメソッド |
|---|---|
| `Syncer.fetchIssues(repo)` | `IssueProvider.ListOpenIssues()` |
| `Syncer.fetchComments(repo, num)` | `IssueProvider.FetchComments()` |
| `orchestrator.handlePRMerged()` 内の `gh issue close` | `IssueProvider.CloseIssue()` |
| `FormatID(owner, repo, number)` | `IssueProvider.FormatSystemID()` |
| `ParseID(id)` | `IssueProvider.ParseSystemID()` |
| `ParsePRBodyIssueID(body)` | `ChangeRequestProvider.ParseChangeRequestBody()` |
| `EventWatcher.Run()` | `EventProvider.WatchEvents()` |

---

## 7. 将来プラグインの実装要件

### 7.1. GitLabプラグイン

- `IssueProvider` を実装（GitLab Issues API）
- `ChangeRequestProvider` を実装（GitLab Merge Requests）
- `EventProvider` を実装（GitLab Webhooks または Events API）
- SystemIDフォーマット: `{group}-{project}-{issue_iid}`（例: `mygroup-myproject-42`）

### 7.2. Azure DevOpsプラグイン

- `IssueProvider` を実装（Azure DevOps Work Items API）
- `ChangeRequestProvider` を実装（Azure DevOps Pull Requests）
- `EventProvider` を実装（Azure DevOps Service Hooks または Webhooks）
- SystemIDフォーマット: `{org}-{project}-{work_item_id}`

### 7.3. Google Spreadsheetプラグイン

- `IssueProvider` を実装（Sheets API によるセル読み書き）
- `ChangeRequestProvider` は **実装しない（nil）**
  - オーケストレーターは自動クローズを行わず、手動クローズモードで動作
- `EventProvider` は実装しない（ポーリングのみ）
  - Google Sheets APIはWebhook/Events APIを持たないため
- スプレッドシートのスキーマ（列定義）:
  - A列: イシューID（MADFLOWシステムID）
  - B列: タイトル
  - C列: ステータス（open / in_progress / closed）
  - D列: 担当チーム番号
  - E列: 本文
  - F列: 最終更新日時

---

## 8. 実装ロードマップ

本イシューのスコープは**設計ドキュメントの作成のみ**であり、以下の各ステップは別途イシューとして管理する。

| フェーズ | 内容 | 依存 |
|---|---|---|
| Phase 1 | プラグインインターフェース定義（`internal/integration/plugin.go`）の実装 | なし |
| Phase 2 | 組み込みGitHubプラグインのリファクタリング（`internal/github/` → `internal/integration/builtin/github/`） | Phase 1 |
| Phase 3 | オーケストレーターのプラグイン対応リファクタリング | Phase 2 |
| Phase 4 | `madflow.toml` の設定スキーマ拡張（後方互換性維持） | Phase 3 |
| Phase 5 | GitLabプラグイン実装 | Phase 4 |
| Phase 6 | Azure DevOpsプラグイン実装 | Phase 4 |
| Phase 7 | Google Spreadsheetプラグイン実装 | Phase 4 |

---

## 9. まとめ

本実装案のポイントは以下の通り。

1. **3層のインターフェース分離**: `IssueProvider`（必須）、`ChangeRequestProvider`（オプション）、`EventProvider`（オプション）に分離することで、機能セットの異なるプラットフォームに柔軟に対応できる。

2. **組み込みGitHubプラグインの維持**: 現在のGitHubインテグレーションをそのまま組み込みプラグインとして維持し、既存ユーザーへの影響を最小化する。

3. **Pull Request非対応プラットフォームへの対応**: `ChangeRequestProvider` を実装しないプラグインに対しては、オーケストレーターが手動クローズモードで動作し、ChangeRequest機能への依存なくワークフローを継続できる。

4. **後方互換性の確保**: 既存の `madflow.toml` の `[github]` セクションを自動的に `[integration.github]` として読み込むフォールバック処理により、既存設定ファイルの変更なしに移行できる。

5. **段階的な移行**: フェーズを分けた実装ロードマップにより、現行機能を壊すことなく段階的にプラグインアーキテクチャへ移行できる。
