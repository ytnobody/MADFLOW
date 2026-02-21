# MADFLOW

MADFLOW（Multi-Agent Development Flow）は、複数の AI エージェントがチームとして協調し、ソフトウェア開発を自律的に進める開発フレームワークです。

## 特徴

- **動的特務チーム型アーキテクチャ**: 監督・エンジニア・レビュアー・リリースマネージャーの4つのロールが連携
- **自律的なタスク管理**: イシューの作成から設計・実装・レビュー・リリースまでを AI エージェントが遂行
- **コンテキストリセット機能**: AIの性能低下を防ぐ自動リフレッシュ機構
- **Git/GitHub 統合**: ブランチ戦略・イシュー同期を自動管理

## 必要要件

- Go 1.23 以上
- Git
- [Claude Code](https://claude.com/claude-code)（`claude` コマンドが利用可能であること）
- GitHub CLI（`gh`）（GitHub Issue 同期を使用する場合）

## インストール

```bash
go install github.com/ytnobody/madflow/cmd/madflow@latest
```

## クイックスタート

### 1. プロジェクトの初期化

```bash
cd your-project
madflow init
```

`madflow.toml` が生成されます。必要に応じて設定を編集してください。

### 2. エージェントの起動

```bash
madflow start
```

### 3. イシューの作成

```bash
madflow issue create "機能Xを実装する"
```

### 4. 状態の確認

```bash
madflow status   # エージェントとチームの状態表示
madflow logs     # チャットログのリアルタイム表示
```

### 5. エージェントの停止

```bash
madflow stop
```

## 設定

プロジェクトルートの `madflow.toml` で設定を管理します。

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
engineer = "claude-sonnet-4-6"
reviewer = "claude-sonnet-4-6"
release_manager = "claude-haiku-4-5"

[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"
```

### GitHub Issue 同期（オプション）

```toml
[github]
owner = "myorg"
repos = ["my-app"]
sync_interval_minutes = 5
```

## コマンド一覧

| コマンド | 説明 |
|---------|------|
| `madflow init` | プロジェクトを初期化 |
| `madflow start` | 全エージェントを起動 |
| `madflow status` | エージェントとチームの状態を表示 |
| `madflow logs` | チャットログをリアルタイム表示 |
| `madflow stop` | 全エージェントを停止 |
| `madflow issue create <title>` | イシューを作成 |
| `madflow issue list` | イシュー一覧を表示 |
| `madflow issue show <id>` | イシュー詳細を表示 |
| `madflow issue close <id>` | イシューをクローズ |
| `madflow release` | develop を main にマージ |
| `madflow sync` | GitHub Issue を手動同期 |

## アーキテクチャ

詳細な要件定義は [SPEC.md](./SPEC.md) を、実装計画は [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) を参照してください。

## ライセンス

（ライセンスファイルが追加され次第記載）
