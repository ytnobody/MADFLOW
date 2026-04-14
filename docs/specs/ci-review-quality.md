# CI・レビュー品質強化仕様

## 概要

人間によるPRレビューを不要にするため、CI・テスト品質・AIレビュー観点を強化する。

## 1. CI強化

### 1.1 静的解析 (golangci-lint)

- `golangci-lint` を CI に追加する
- 設定ファイル `.golangci.yml` でチェック項目を管理する
- 有効にする linter:
  - `errcheck`: エラー戻り値の無視を検出
  - `govet`: vet チェック
  - `staticcheck`: 静的解析
  - `gosimple`: コードの単純化提案
  - `unused`: 未使用コード検出
  - `ineffassign`: 無効な代入検出
  - `gofmt`: フォーマットチェック（既存の fmt check と統合）

### 1.2 セキュリティスキャン (govulncheck)

- `govulncheck` を CI に追加する
- 既知の脆弱性を持つ依存パッケージを検出する
- CI 失敗条件: 脆弱性が検出された場合

### 1.3 テストカバレッジ閾値

- テストカバレッジが閾値を下回った場合に CI を失敗させる
- 閾値: **60%** (初期値)
- `go test -coverprofile` でカバレッジを計測し、`go tool cover` で閾値チェック

### 1.4 インテグレーションテスト

- `go test -tags integration ./...` でインテグレーションテストを実行する
- インテグレーションテストは `//go:build integration` ビルドタグで分離する
- CI では通常テストとは別ステップで実行する（失敗は `continue-on-error: true` で警告扱い）

## 2. Superintendentのレビュー観点の明文化

### 2.1 CLAUDE.md

プロジェクトルートに `CLAUDE.md` を作成し、AIエージェント向けのレビューチェックリストを追加する。

### 2.2 レビューチェックリスト項目

#### エラーハンドリング
- すべてのエラー戻り値を適切に処理すること (`errcheck`)
- エラーをラップする際は `fmt.Errorf("...: %w", err)` を使用すること
- 上位へのエラー伝播は適切に行うこと

#### タイムアウト設定
- 外部プロセス呼び出し (`exec.Command`) には `context` を使用してタイムアウトを設定すること
- HTTP クライアントには必ず `Timeout` フィールドを設定すること
- goroutine のリーク防止のため、長時間実行する処理には `ctx.Done()` の監視を行うこと

#### 命名規則
- Go の標準命名規則に従うこと（`camelCase`, エクスポートは `PascalCase`）
- パッケージ名は小文字のみ使用すること
- インターフェース名は動詞 + `er` サフィックスを使用すること（例: `Reader`, `Writer`）

#### テスト品質
- ユニットテストは依存を最小化し、外部 I/O はモックすること
- テスト名は `Test<FunctionName>_<scenario>` 形式を推奨
- エッジケース・エラーパスをカバーすること

#### セキュリティ
- 外部入力はパストラバーサルを防止するためにサニタイズすること
- 機密情報（APIキー等）をハードコードしないこと
- `govulncheck` で脆弱な依存が存在しないことを確認すること

## 3. リスクレベルに応じたマージ戦略

### 3.1 リスク判定ロジック (`internal/risk` パッケージ)

変更のリスクレベルを以下の基準で判定する:

#### HIGH リスク
以下のいずれかに該当する場合:
- 変更ファイル数が 20 以上
- 変更行数 (追加+削除) が 500 以上
- `cmd/` 配下のファイルに変更がある (エントリーポイント変更)
- `go.mod` / `go.sum` に変更がある (依存変更)
- `.github/workflows/` に変更がある (CI変更)
- GitHub ラベルに `high-risk` が付いている

#### MEDIUM リスク
以下のいずれかに該当し、HIGH でない場合:
- 変更ファイル数が 10 以上
- 変更行数が 200 以上
- `internal/orchestrator/` または `internal/config/` に変更がある (コアロジック変更)
- GitHub ラベルに `medium-risk` が付いている

#### LOW リスク
HIGH / MEDIUM に該当しない場合:
- ドキュメントのみの変更 (`docs/`, `*.md`)
- テストのみの変更 (`*_test.go`)
- 設定ファイルの軽微な変更

### 3.2 マージ戦略

| リスクレベル | マージ方針 |
|---|---|
| LOW | Superintendent が自動承認・自動マージ |
| MEDIUM | Superintendent が自動承認・自動マージ + チャットログに事後確認メモを残す |
| HIGH | Superintendent がレビューを行い、`[HUMAN REVIEW REQUIRED]` コメントを GitHub に投稿して人間レビューを要請 |

### 3.3 リスク判定 API

```go
package risk

// Level represents the risk level of a PR change.
type Level int

const (
    LOW    Level = iota // 低リスク: 完全自動マージ
    MEDIUM              // 中リスク: 自動マージ + 事後確認
    HIGH                // 高リスク: 人間レビュー必須
)

// Evaluator evaluates the risk level of a PR.
type Evaluator interface {
    Evaluate(pr PRInfo) Level
}

// PRInfo holds the metadata needed for risk evaluation.
type PRInfo struct {
    FilesChanged  int
    LinesAdded    int
    LinesDeleted  int
    ChangedPaths  []string
    Labels        []string
}
```

### 3.4 Superintendentプロンプトへの組み込み

Superintendent の `prompts/superintendent.md` にリスク判定基準とマージ戦略を追記する。
orchestrator の `runIssuePatrol` からリスク評価結果を patrol メッセージに含める（将来拡張）。
