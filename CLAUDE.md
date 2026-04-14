# CLAUDE.md — MADFLOWプロジェクト AIエージェント向けガイドライン

このファイルはMADFLOWプロジェクトにおけるAIエージェント（Claude Code）の動作に関するプロジェクト固有のガイドラインです。

## PRレビューチェックリスト

Superintendentは以下のチェックリストに基づいてPRをレビューしてください。

### ✅ エラーハンドリング

- [ ] すべてのエラー戻り値を適切に処理している（`_` で無視していない）
- [ ] エラーをラップする際は `fmt.Errorf("コンテキスト: %w", err)` を使用している
- [ ] 上位へのエラー伝播が適切に行われている
- [ ] エラーメッセージは具体的で原因が特定できる内容になっている

### ✅ タイムアウト設定

- [ ] 外部プロセス呼び出し (`exec.Command`) には `context.WithTimeout` を使用している
- [ ] HTTP クライアントには `Timeout` フィールドが設定されている
- [ ] 長時間実行するgoroutineは `ctx.Done()` をselect文で監視している
- [ ] ブロッキング操作には適切なタイムアウトが設定されている

### ✅ 命名規則

- [ ] パッケージ名は小文字のみ（例: `risk`, `orchestrator`）
- [ ] エクスポートされる識別子は `PascalCase`
- [ ] 非エクスポートの識別子は `camelCase`
- [ ] インターフェースは動詞 + `er` サフィックス（例: `Evaluator`, `Manager`）
- [ ] テスト関数名は `Test<FunctionName>` または `Test<FunctionName>_<scenario>` 形式

### ✅ テスト品質

- [ ] 変更した機能に対応するテストが存在する
- [ ] テストは外部依存（ファイルI/O、ネットワーク、プロセス）をモックまたは最小化している
- [ ] エッジケースとエラーパスがカバーされている
- [ ] テストは決定論的（実行順序や環境に依存しない）

### ✅ セキュリティ

- [ ] 外部入力（ファイルパス、ユーザー入力）はパストラバーサルなど攻撃に対してサニタイズされている
- [ ] APIキー・パスワード等の機密情報がハードコードされていない
- [ ] `govulncheck` で既知の脆弱性を持つ依存が存在しないことを確認
- [ ] 外部コマンド実行時は入力をサニタイズしている

### ✅ コード品質

- [ ] `go vet` / `golangci-lint` でエラーが発生しない
- [ ] `gofmt` でフォーマット済み
- [ ] 未使用の変数・インポートが存在しない
- [ ] TODOコメントが残っていない

---

## リスクレベルに応じたマージ戦略

PRをレビューする際は、以下の基準でリスクレベルを判定し、適切なマージ戦略を取ってください。

### HIGH リスク → 人間レビュー必須

以下のいずれかに該当する場合:
- 変更ファイル数が **20以上**
- 変更行数（追加+削除）が **500以上**
- `cmd/` 配下のファイルに変更あり（エントリーポイント変更）
- `go.mod` / `go.sum` に変更あり（依存変更）
- `.github/workflows/` に変更あり（CI変更）
- GitHubラベルに `high-risk` が付いている

**対応**: GitHub Issueに `[HUMAN REVIEW REQUIRED]` コメントを投稿し、人間のレビューを待つ。

### MEDIUM リスク → 自動マージ + 事後確認

以下のいずれかに該当し、HIGHでない場合:
- 変更ファイル数が **10以上**
- 変更行数が **200以上**
- `internal/orchestrator/` または `internal/config/` に変更あり
- GitHubラベルに `medium-risk` が付いている

**対応**: CIが通れば自動マージ。チャットログに事後確認メモを残す。

### LOW リスク → 完全自動マージ

上記のいずれにも該当しない場合。

**対応**: CIが通れば即座に自動マージ。

---

## Go コーディング規約

### パッケージ構成

```
internal/
  <feature>/
    <feature>.go       # 実装
    <feature>_test.go  # テスト
```

### エラー処理パターン

```go
// 推奨: エラーをラップして文脈を保持する
result, err := doSomething()
if err != nil {
    return fmt.Errorf("doSomething failed: %w", err)
}

// 非推奨: エラーを無視する
result, _ := doSomething()
```

### context 使用パターン

```go
// 推奨: タイムアウト付きcontext
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
cmd := exec.CommandContext(ctx, "gh", "pr", "view")
```

### テストパターン

```go
// 推奨: テーブル駆動テスト
func TestEvaluate(t *testing.T) {
    tests := []struct {
        name string
        input SomeType
        want  SomeResult
    }{
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```
