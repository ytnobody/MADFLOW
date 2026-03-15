# Spec: TODO実装の抑止 (No TODO Implementation)

## 概要

Goソースコード内に `// TODO` コメント（未実装のプレースホルダー）が含まれていた場合、CIおよびlintチェックで検出し、マージを防止する。

## 背景

`// TODO: implement` のような未実装のプレースホルダーがコードに残ったままマージされることを防ぐ必要がある。

## 対象

- `**/*.go` ファイル内のすべての `// TODO` コメント

## チェック仕様

### 検出対象パターン

以下のパターンをGoソースファイル内で検出する（大文字小文字を区別する）:

- `// TODO`（行コメント形式のTODO）
- `/* TODO`（ブロックコメント形式のTODO）

### チェック範囲

- プロジェクト内の全Goソースファイル（`*.go`）
- vendorディレクトリは除外する

### 動作

- TODO コメントが検出された場合: エラーを出力してexit code 1で終了
- TODO コメントが検出されない場合: 正常終了（exit code 0）

## 実装方法

### 1. Makefile の lint ターゲットに追加

`make lint` 実行時にTODOチェックを行う。

### 2. CI ワークフロー (.github/workflows/ci.yml) に追加

CIパイプラインの lint ステップとしてTODOチェックを追加する。

## エラーメッセージ例

```
TODO comments found in Go source files:
internal/foo/bar.go:42: // TODO: implement this
Please remove or implement all TODO items before merging.
```

## 除外対象

- テストファイル（`*_test.go`）は除外しない（テストコードにもTODOは残すべきでない）
- `vendor/` ディレクトリは除外する
