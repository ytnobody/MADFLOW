# MADFLOW

MADFLOW（Multi-Agent Development Flow）は、複数の AI エージェントがチームとして協調し、ソフトウェア開発を自律的に進める開発フレームワークです。

## 特徴

- **シンプルな2エージェント構成**: 監督とエンジニアのみで構成
- **監督の一元管理**: 監督がPM・設計・レビュー・マージを統括
- **自律的なタスク管理**: イシューの作成から実装・レビュー・マージまでを AI エージェントが遂行
- **コンテキストリセット機能**: AIの性能低下を防ぐ自動リフレッシュ機構
- **Git/GitHub 統合**: ブランチ戦略・イシュー同期を自動管理

## 必要要件

- Go 1.25 以上
- Git
- 以下のいずれか:
  - [Claude Code](https://claude.com/claude-code)（`claude` コマンド）
  - [gmn](https://github.com/tomohiro-owada/gmn)（`gmn` コマンド）- Gemini モデル使用時
- GitHub CLI（`gh`）（GitHub Issue 同期を使用する場合）

## インストール

### go install を使用する場合

```bash
go install github.com/ytnobody/madflow/cmd/madflow@latest
```

### GitHub Releases からバイナリをダウンロードする場合

[GitHub Releases](https://github.com/ytnobody/madflow/releases/latest) から、お使いのOSとアーキテクチャに対応したバイナリをダウンロードしてください。

```bash
# Linux (amd64) の例
curl -L https://github.com/ytnobody/madflow/releases/latest/download/madflow-linux-amd64 -o madflow
chmod +x madflow
sudo mv madflow /usr/local/bin/
```

インストール後は `madflow upgrade` コマンドで最新バージョンへのアップグレードも可能です。

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
# Gemini モデルも使用可能:
# superintendent = "gemini-2.0-flash-exp"
# engineer = "gemini-2.5-pro"

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
| `madflow version` | 現在のバージョンを表示 |
| `madflow upgrade` | madflow を最新バージョンにアップグレード |

## アーキテクチャ

詳細な要件定義は [SPEC.md](./SPEC.md) を、実装計画は [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) を参照してください。

## ライセンス

MIT License
