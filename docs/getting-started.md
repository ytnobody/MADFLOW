# MADFLOW 導入ガイド

このドキュメントは、MADFLOW の導入を検討しているエンジニア・チーム向けに、概要・導入手順・利用方法・セキュリティ上の注意点・ベストプラクティスをまとめたものです。

---

## 1. MADFLOW とは何か

MADFLOW（Multi-Agent Development Flow）は、複数の AI エージェントがチームとして協調しながらソフトウェア開発を自律的に進める **開発自動化フレームワーク**です。

### 基本的な仕組み

MADFLOW は **Superintendent（監督）** と **Engineer（エンジニア）** という 2 種類のエージェントで構成されています。

```
Human（あなた）
    ↕ GitHub Issue / コメント
Superintendent（AI）
    ↕ チャットログ（ローカルファイル）
Engineer × N（AI）
    ↕ Git / GitHub
コードベース
```

| エージェント | 役割 |
|---|---|
| **Superintendent** | PM・アーキテクト・レビュアー・リリースマネージャーを兼務。イシューの割り当てから PR のマージまでを担う |
| **Engineer** | Superintendent の指示に基づいてコードを実装。仕様書作成→テスト作成→実装のドキュメント駆動開発サイクルに従う |

あなた（Human）は GitHub Issue を通じて要件・バグ・依頼を書き込むだけで、AI エージェントが自律的に実装・レビュー・マージを行います。

---

## 2. 導入するとどうなるか

### できるようになること

- **Issue を書くだけでコードが生成される**: GitHub Issue に要件を記述すると、AI エージェントがブランチ作成・実装・PR 作成・レビュー・マージまでを自律的に実施します
- **並列開発の自動管理**: 複数の Issue を同時に処理できます（Engineer を複数起動）
- **ドキュメント駆動開発の自動実践**: 実装前に仕様書・テストを自動生成するため、コード品質が安定します
- **コンテキストリセットによる品質維持**: 8 分ごとに AI のコンテキストを自動リフレッシュし、性能劣化を防ぎます

### 想定ユースケース

| ユースケース | 説明 |
|---|---|
| **個人プロジェクトの加速** | 一人で複数の機能開発を並行して進めたい |
| **小規模チームの開発支援** | 人手不足を AI エージェントで補い、開発速度を向上させたい |
| **ルーティン実装の自動化** | CRUD 操作・テスト追加・ドキュメント整備などの定型作業を委任したい |
| **OSS への貢献支援** | Issue ドリブンで継続的な改善を自動化したい |

---

## 3. 導入するのに必要なもの

### 必須要件

| 要件 | 説明 |
|---|---|
| **Go 1.25 以上** | MADFLOW 本体のビルド・実行に必要 |
| **Git** | ブランチ管理・コミット操作に使用 |
| **AI バックエンド（いずれか 1 つ）** | 下記の表を参照 |

### AI バックエンド（いずれか 1 つを選択）

| バックエンド | 必要なもの | 費用感 |
|---|---|---|
| **Claude CLI**（推奨） | Claude Code（Pro/Max サブスクリプション）、`claude` コマンド | 月額固定（¥15,000 前後） |
| **Gemini CLI** | `gemini-cli` コマンド（無料枠あり） | 無料〜従量課金 |
| **Anthropic API キー** | `ANTHROPIC_API_KEY` 環境変数 | 従量課金（¥1,000〜¥8,000/月、使用量による） |

### オプション要件

| 要件 | 説明 |
|---|---|
| **GitHub CLI (`gh`)** | GitHub Issue との同期・PR 操作に必要（GitHub 連携を使う場合） |
| **GitHub アカウント** | Issue/PR の管理に使用（GitHub 連携を使う場合） |

---

## 4. 導入する方法

### ステップ 1: MADFLOW のインストール

**方法 A: `go install` でインストール（推奨）**

```bash
go install github.com/ytnobody/madflow/cmd/madflow@latest
```

**方法 B: バイナリを直接ダウンロード**

```bash
# Linux (amd64) の例
curl -L https://github.com/ytnobody/madflow/releases/latest/download/madflow-linux-amd64 -o madflow
chmod +x madflow
sudo mv madflow /usr/local/bin/
```

macOS や Windows など他の OS・アーキテクチャは [GitHub Releases](https://github.com/ytnobody/madflow/releases/latest) からダウンロードしてください。

インストール後、以下のコマンドでバージョンを確認できます。

```bash
madflow version
```

### ステップ 2: プロジェクトの初期化

MADFLOW で管理したいプロジェクトのディレクトリで `madflow init` を実行します。

```bash
cd your-project
madflow init
```

`madflow.toml` が生成されます。必要に応じて編集してください。

### ステップ 3: 設定ファイルの編集

生成された `madflow.toml` を編集します。以下は基本設定の例です。

```toml
# プロジェクト名と管理するリポジトリ
[project]
name = "my-app"

[[project.repos]]
name = "main"
path = "."

# コンテキストリセット間隔（分）
[agent]
context_reset_minutes = 8

# 使用するモデル
[agent.models]
superintendent = "claude-opus-4-7"
engineer = "claude-sonnet-4-6"

# ブランチ設定
[branches]
main = "main"
develop = "develop"
feature_prefix = "feature/issue-"

# GitHub 連携（オプション）
# authorized_users は GitHub 連携を有効にする場合に必須
authorized_users = ["your-github-username"]

[github]
owner = "your-org"
repos = ["your-repo"]
sync_interval_minutes = 5
```

> **重要**: GitHub 連携を有効にする場合は、`authorized_users` を必ず設定してください。詳細は「[6. 導入時のセキュリティチェック](#6-導入時のセキュリティチェック)」を参照してください。

### ステップ 4: AI バックエンドのセットアップ

使用する AI バックエンドに応じて、以下のいずれかを実施してください。

**Claude CLI（Claude Code）を使う場合**

```bash
# Claude Code がインストール済みであること（https://claude.com/claude-code）
# 自動的に認証情報が使用されます
madflow use claude
```

**Gemini CLI を使う場合**

```bash
# gemini-cli がインストール済みであること（https://github.com/google-gemini/gemini-cli）
madflow use gemini
```

**Anthropic API キーを使う場合**

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
madflow use claude-api-standard
```

### ステップ 5: エージェントの起動

```bash
madflow start
```

MADFLOW が起動し、Superintendent と Engineer エージェントが動作を開始します。

---

## 5. 利用方法

### 基本的な使い方

MADFLOW を起動した後は、GitHub Issue（または MADFLOW 内部のイシューファイル）に要件を書き込むことでエージェントに作業を依頼します。

#### GitHub Issue での依頼（推奨）

GitHub Issue を通常どおり作成します。Superintendent が自動的に検知して割り当てを行います。

```
タイトル: ユーザー登録機能を追加する
本文: ユーザーのメールアドレスとパスワードで登録できるエンドポイントを追加してください。
      バリデーション: メールアドレスの形式チェック、パスワードは8文字以上。
```

#### コマンドリファレンス

| コマンド | 説明 |
|---|---|
| `madflow init` | プロジェクトを初期化し、`madflow.toml` を生成する |
| `madflow start` | Superintendent・Engineer エージェントを起動する |
| `madflow use <preset>` | 使用するモデルプリセットを切り替える |
| `madflow version` | 現在のバージョンを表示する |
| `madflow upgrade` | madflow を最新バージョンにアップグレードする |

#### モデルプリセット一覧

```bash
# Claude CLI（Pro/Max サブスクリプション必須）
madflow use claude          # claude-sonnet / claude-sonnet
madflow use claude-cheap    # claude-sonnet / claude-haiku（低コスト）

# Gemini CLI
madflow use gemini          # gemini-2.5-pro / gemini-2.5-pro
madflow use gemini-cheap    # gemini-2.5-flash / gemini-2.5-flash（高速・低コスト）

# ハイブリッド（監督: Claude、エンジニア: Gemini）
madflow use hybrid
madflow use hybrid-cheap

# Anthropic API キー直接利用（サブスクリプション不要）
madflow use claude-api-standard   # sonnet / haiku
madflow use claude-api-cheap      # haiku / haiku（最安）
```

#### バージョンアップ

```bash
madflow upgrade
```

最新バイナリが自動的にダウンロードされ、インストールされます。

### エージェントとのコミュニケーション

MADFLOW では、チャットログ（`~/.madflow/<project-name>/chatlog.txt`）を通じてエージェント間の通信が行われます。進捗確認はこのファイルを参照してください。

```bash
# チャットログをリアルタイムで確認
tail -f ~/.madflow/my-app/chatlog.txt
```

### イシューの確認

```bash
# イシューファイルの一覧
ls ~/.madflow/my-app/issues/

# 特定のイシューの状態確認
cat ~/.madflow/my-app/issues/<issueID>.toml
```

---

## 6. 導入時のセキュリティチェック

MADFLOW は AI エージェントが自律的にコマンドを実行するため、セキュリティリスクを理解した上で導入することが重要です。

### チェックリスト

導入前に以下を必ず確認してください。

#### [ ] `authorized_users` を設定する（GitHub 連携使用時は必須）

```toml
# madflow.toml に必ず設定する
authorized_users = ["your-github-username", "trusted-collaborator"]
```

**なぜ必要か**: 未設定の場合、公開リポジトリに対してどの GitHub ユーザーでも Issue を作成・コメントでき、MADFLOW がそれを処理します。悪意のある Issue によってプロンプトインジェクション攻撃を受けるリスクがあります。

#### [ ] プライベートリポジトリでの使用を検討する

公開リポジトリで MADFLOW を使用する場合、`authorized_users` の設定は特に重要です。信頼できるユーザーのみを登録してください。

#### [ ] AI エージェントの実行権限を把握する

MADFLOW のエージェントは、ホストマシン上でコマンドを実行する権限を持ちます（Claude CLI バックエンドでは `--dangerously-skip-permissions` フラグを使用）。以下の点に注意してください。

- エージェントは Git 操作・ファイル編集・ビルドコマンドなどを自律的に実行します
- 本番環境のサーバーや重要なシステムにアクセスできる環境では直接実行しないことを推奨します
- 専用の開発環境・CI 環境での実行を推奨します

#### [ ] データディレクトリのパーミッションを確認する

```bash
# データディレクトリのパーミッションを確認
ls -la ~/.madflow/

# 推奨: 本人のみアクセス可能に設定
chmod 700 ~/.madflow/
chmod 600 ~/.madflow/*/chatlog.txt
```

デフォルトでは `0755` / `0644` で作成されるため、同一ホストの他ユーザーからチャットログ（Issue 内容・LLM との会話ログ）が読み取れる状態になっています。共有サーバーで使用する場合は特に注意してください。

#### [ ] API キーの管理

```bash
# API キーは環境変数で渡す（ファイルへの直接記述は避ける）
export ANTHROPIC_API_KEY="sk-ant-..."
export GOOGLE_API_KEY="..."  # Gemini 使用時

# .env ファイルを使う場合は .gitignore に追加する
echo ".env" >> .gitignore
```

#### [ ] バイナリの整合性確認

`go install` を使わずバイナリを直接ダウンロードした場合は、公式リリースページの SHA256 チェックサムで整合性を確認してください。

```bash
# ダウンロードしたバイナリのチェックサムを確認
sha256sum madflow
```

---

## 7. 実運用におけるベストプラクティス

### Issue の書き方

AI エージェントが正確に実装するために、Issue には以下を含めることを推奨します。

```markdown
## 概要
（何を実現したいかを 1〜2 文で）

## 要件
- 要件 1
- 要件 2

## 受け入れ条件
- [ ] 条件 A を満たすこと
- [ ] 条件 B を満たすこと

## 補足
（実装方針のヒント・参照すべき既存コードなど）
```

**避けるべき書き方**:
- 曖昧な表現（「いい感じにして」「適切に修正する」）
- スコープが広すぎる Issue（複数の独立した機能を 1 つの Issue に詰め込む）
- 悪意ある内容・プロンプトインジェクションを誘発するコンテンツ

### ブランチ戦略の維持

MADFLOW は `main` / `develop` / `feature/issue-<ID>` の 3 層ブランチ戦略を前提としています。

```bash
# develop ブランチを常に最新に保つ
git checkout develop
git pull origin develop

# 本番マージは main ← develop のみ（直接 feature → main は避ける）
```

### コンテキストリセット間隔の調整

デフォルトは 8 分ですが、Issue の複雑さに応じて調整してください。

```toml
[agent]
context_reset_minutes = 8   # 複雑な Issue が多い場合は 10〜15 に増やす
```

### チャットログの定期メンテナンス

チャットログは増加し続けるため、定期的にアーカイブ・削除することを推奨します。

```bash
# チャットログのサイズ確認
du -sh ~/.madflow/*/chatlog.txt

# 古いログのアーカイブ（例: 30 日以上前）
find ~/.madflow/ -name "chatlog.txt" -mtime +30 -exec gzip {} \;
```

### モデルプリセットの使い分け

| シーン | 推奨プリセット |
|---|---|
| 本格的な機能開発 | `claude` / `gemini` |
| コスト最適化 | `claude-cheap` / `gemini-cheap` |
| サブスクリプションなし | `claude-api-standard` / `claude-api-cheap` |
| 無料で試したい | `gemini-cheap`（Gemini Flash は無料枠あり） |

### PR・マージのレビュー習慣

AI が生成したコードも必ず人間がレビューすることを推奨します。

- **自動マージを無効化**: GitHub の Branch Protection Rules でマージに人間の承認を必須にする
- **差分の確認**: AI が予期しない変更を加えていないか確認する
- **テストの実行**: CI が通過しているか確認する

### 定期的なアップグレード

```bash
# MADFLOW 本体のアップグレード
madflow upgrade

# AI モデルの最新プリセットを確認
madflow use --list  # 利用可能なプリセット一覧を確認
```

---

## 参考リンク

- [MADFLOW GitHub リポジトリ](https://github.com/ytnobody/madflow)
- [SPEC.md（アーキテクチャ仕様）](./SPEC.md)
- [SECURITY_AUDIT_REPORT.md（セキュリティ調査レポート）](./SECURITY_AUDIT_REPORT.md)
- [GitHub Releases](https://github.com/ytnobody/madflow/releases)
