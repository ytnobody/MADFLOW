# Issue Domain Model Spec

## Overview

`internal/issue` パッケージのドメインモデルを関数型ドメインモデリングの観点で強化する。
主な変更点は `Status` の sum type 化、状態遷移関数の追加、純関数としての Comment 操作関数の追加。

## Status 型

### 定義

`Status` は `int` ベースの sum type として定義される。

```go
type Status int

const (
    StatusOpen       Status = iota // "open"
    StatusInProgress               // "in_progress"
    StatusResolved                 // "resolved"
    StatusClosed                   // "closed"
)
```

### メソッド

- `String() string`: ステータスを人間が読める文字列に変換（"open", "in_progress", "resolved", "closed"）
- `IsTerminal() bool`: 終端ステータス（StatusResolved, StatusClosed）の場合 true を返す
- `MarshalText() ([]byte, error)`: TOML/JSON シリアライズのために文字列に変換
- `UnmarshalText(b []byte) error`: TOML/JSON デシリアライズのために文字列から変換

### TOML 互換性

TOML ファイルにはこれまで通り `"open"`, `"in_progress"`, `"resolved"`, `"closed"` の文字列で保存される。
`encoding.TextMarshaler` / `encoding.TextUnmarshaler` インターフェースを実装することで互換性を維持する。

### 未知のステータス文字列

`UnmarshalText` で未知の文字列が渡された場合、`fmt.Errorf` でエラーを返す。

## 状態遷移関数

状態遷移は純関数（value copy を返す）として実装される。直接フィールド代入は引き続き可能だが、
明示的な状態遷移には以下の関数を使用することを推奨する。

### `TransitionToInProgress(iss Issue, teamID int) (Issue, error)`

- 前提条件: `iss.Status` が終端ステータスでないこと
- 効果: `Status = StatusInProgress`, `AssignedTeam = teamID`
- エラー: 終端ステータスからの遷移は `fmt.Errorf` でエラーを返す

### `TransitionToOpen(iss Issue) (Issue, error)`

- 前提条件: `iss.Status` が `StatusInProgress` であること
- 効果: `Status = StatusOpen`, `AssignedTeam = 0`
- エラー: `StatusInProgress` 以外からの遷移はエラーを返す

### `TransitionToResolved(iss Issue) (Issue, error)`

- 前提条件: `iss.Status` が `StatusInProgress` であること
- 効果: `Status = StatusResolved`
- エラー: `StatusInProgress` 以外からの遷移はエラーを返す

### `TransitionToClosed(iss Issue) (Issue, error)`

- 前提条件: なし（どのステータスからも遷移可能）
- 効果: `Status = StatusClosed`

## Smart Constructor

```go
func NewIssue(id, title, body string) *Issue
```

`Status = StatusOpen`, `AssignedTeam = 0` をデフォルト値とする Issue を生成して返す。

## MergeComments 純関数

```go
func MergeComments(comments []Comment, c Comment) ([]Comment, bool)
```

既存の `AddComment` メソッドに対応する純関数バージョン。
- `comments` スライスに `c` が含まれない場合（ID で重複チェック）、新しいスライスに `c` を追加して返す（true）
- 既に含まれている場合は元のスライスをそのまま返す（false）
- 元の `comments` スライスを変更しない（イミュータブル）

## 既存 API の後方互換性

- `AddComment(c Comment) bool`: 維持（引き続き使用可能）
- `HasComment(id int64) bool`: 維持
- `Issue` フィールドはすべて公開のまま（TOML シリアライズとの互換性のため）
- `StatusFilter` 構造体: 変更なし

## ファイル構成

```
internal/issue/
  issue.go       # Status sum type, 状態遷移関数, MergeComments
  issue_test.go  # 新機能のテスト追加
```
