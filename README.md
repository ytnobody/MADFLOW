<p align="center">
  <img src="https://github.com/user-attachments/assets/77200c22-452b-459e-84b5-88ab0dc80703" alt="MADFLOW Logo" width="400">
</p>

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
- 以下のいずれか（使用するバックエンドによって異なります）:
  - [Claude Code](https://claude.com/claude-code)（`claude` コマンド）- Claude CLI バックエンド使用時
  - [gmn](https://github.com/tomohiro-owada/gmn)（`gmn` コマンド）- Gemini モデル使用時
  - `ANTHROPIC_API_KEY` 環境変数 - Anthropic API キーバックエンド使用時（追加インストール不要）
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
# Anthropic API キー方式（ANTHROPIC_API_KEY 環境変数が必要）:
# superintendent = "anthropic/claude-sonnet-4-5"
# engineer = "anthropic/claude-haiku-4-5"

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
| `madflow use <preset>` | モデルプリセットを切り替え |
| `madflow version` | 現在のバージョンを表示 |
| `madflow upgrade` | madflow を最新バージョンにアップグレード |

## モデルプリセット

`madflow use <preset>` コマンドで使用するモデルを切り替えられます。

| プリセット | superintendent | engineer | 備考 |
|-----------|---------------|----------|------|
| `claude` | claude-sonnet-4-6 | claude-sonnet-4-6 | Claude CLI（Pro/Max必要）|
| `claude-cheap` | claude-sonnet-4-6 | claude-haiku-4-5 | Claude CLI コスト削減版 |
| `gemini` | gemini-pro-2-5 | gemini-pro-2-5 | Gemini CLI（gmn必要）|
| `gemini-cheap` | gemini-flash-2-5 | gemini-flash-2-5 | Gemini 高速・低コスト版 |
| `hybrid` | claude-sonnet-4-6 | gemini-pro-2-5 | ハイブリッド構成 |
| `hybrid-cheap` | claude-sonnet-4-6 | gemini-flash-2-5 | ハイブリッド低コスト版 |
| `claude-api-standard` | anthropic/claude-sonnet-4-5 | anthropic/claude-haiku-4-5 | **Anthropic API キー方式** |
| `claude-api-cheap` | anthropic/claude-haiku-4-5 | anthropic/claude-haiku-4-5 | **Anthropic API キー方式・最安** |

### Anthropic API キー方式の使い方

`claude-api-*` プリセットは Claude Code CLI の代わりに `ANTHROPIC_API_KEY` を使って Anthropic の API を直接呼び出します。

**メリット:**
- Claude Code Pro/Max サブスクリプション不要
- 従量課金でコスト予測可能
- Anthropic のポリシー変更リスクから独立

**セットアップ:**

```bash
# 1. Anthropic API キーを環境変数に設定
export ANTHROPIC_API_KEY="sk-ant-..."

# 2. API キー方式プリセットに切り替え
madflow use claude-api-standard   # 標準品質
# または
madflow use claude-api-cheap      # 最低コスト

# 3. 起動
madflow start
```

### コスト比較（参考）

| 方式 | 月額概算 | 備考 |
|------|---------|------|
| Claude Max (5x) | ¥15,000 | サブスクリプション固定費 |
| `claude-api-standard` | ¥3,000〜8,000 | 従量課金・利用量による |
| `claude-api-cheap` | ¥1,000〜3,000 | Haiku モデル使用 |
| `hybrid-cheap` | gmn 無料枠内 | Gemini Flash は無料枠あり |

> ※ API 料金は 2026 年 2 月時点の概算です。実際の料金は [Anthropic 公式サイト](https://www.anthropic.com/pricing) を参照してください。

## アーキテクチャ

詳細な要件定義は [SPEC.md](./SPEC.md) を、実装計画は [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) を参照してください。

## ライセンス

MIT License
