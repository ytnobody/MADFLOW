# Spec: orchestratorの重複TEAM_CREATE防止メカニズムの強化

## 概要

orchestratorが同一イシューに対して複数回のTEAM_CREATEを実行してしまう問題を防ぐため、
TEAM_CREATEの前に実施するチェックを強化する。

## 背景・問題

以下のような状況でTEAM_CREATEが重複して実行されていた：

- PR提出済みにも関わらず再度TEAM_CREATE
- PR OPEN中にも関わらず再度TEAM_CREATE
- イシューがin_progressでチームが解散済みのときに再度TEAM_CREATE

### 既存チェックの限界

既存の `DecideTeamAssignment` 関数は以下をチェックしていた：

1. ターミナル状態（resolved/closed）→ Reject
2. AssignedTeam > 0（チームが割り当て済み）→ Reject
3. hasActiveTeam（アクティブなチームが存在する）→ Reject

しかし、以下のケースが抜け落ちていた：

**ケース：in_progress + AssignedTeam=0 + アクティブチームなし**

発生経路：
1. エンジニアがPRを作成（status: in_progress, AssignedTeam=5）
2. RC-1修正により、チームが manager に存在しない場合は AssignedTeam をリセット
3. チームが解散している → hasActiveTeam=false
4. AssignedTeam が 0 にリセットされた → AssignedTeam > 0 チェックをパス
5. DecideTeamAssignment が Create を返してしまう → 重複チーム作成！

## 変更仕様

### 1. in_progress イシューの TEAM_CREATE 拒否（`decision.go`）

`DecideTeamAssignment` 関数に、`StatusInProgress` の場合に拒否するチェックを追加する。

**変更前の優先順位：**
1. Terminal status → Reject
2. AssignedTeam > 0 → Reject
3. hasActiveTeam → Reject
4. hasIdleTeam → ReuseIdle
5. atCapacity → Defer
6. Otherwise → Create

**変更後の優先順位：**
1. Terminal status (resolved/closed) → Reject
2. **in_progress status → Reject** ← 新規追加
3. AssignedTeam > 0 → Reject
4. hasActiveTeam → Reject
5. hasIdleTeam → ReuseIdle
6. atCapacity → Defer
7. Otherwise → Create

**拒否メッセージ：**

```
TEAM_CREATE {issueID} は拒否されました: イシューのステータスが in_progress です。
再アサインする場合はイシューのstatusをopenに変更してください
```

**再アサイン手順（通知に含める）：**
- `status = "in_progress"` → `status = "open"` に変更してから再度 TEAM_CREATE

### 2. 既存フィーチャーブランチ/ワークツリーの有無チェック（`orchestrator.go`）

`handleTeamCreate` 関数に、フィーチャーブランチの存在確認チェックを追加する。

`DecideTeamAssignment` を呼び出す前に、設定された全リポジトリに対して：

- ローカルブランチ `{featurePrefix}{issueID}` の存在確認（`BranchExists`）
- リモートトラッキングブランチ `origin/{featurePrefix}{issueID}` の存在確認

いずれかが存在する場合は TEAM_CREATE を拒否し、superintendentに通知する。

**拒否メッセージ：**
```
TEAM_CREATE {issueID} は拒否されました: フィーチャーブランチ {branchName} が
リポジトリ {repoName} に既に存在します
```

また、設定された各リポジトリのワークツリーディレクトリ
`.worktrees/{ghLogin}/issue-{issueID}` が存在する場合も拒否する。

**拒否メッセージ：**
```
TEAM_CREATE {issueID} は拒否されました: ワークツリー {path} が既に存在します
```

## テスト仕様

### decision_test.go の変更

既存のテストケースを変更：
- `"in_progress issue with idle team reuses idle"` → 期待値を `AssignDecisionReject` に変更
- `"in_progress issue with no assignment creates new team"` → 期待値を `AssignDecisionReject` に変更

新規テストケース追加：
- `"in_progress issue is rejected regardless of active team"` - アクティブチームの有無に関わらず in_progress は拒否

### orchestrator_test.go の追加テスト

1. `TestHandleTeamCreateRejectsInProgressNoTeam`
   - in_progress かつ AssignedTeam=0 かつアクティブチームなしのイシューへの TEAM_CREATE を拒否
   - chatlog に拒否メッセージが記録される
   - チーム数が変化しない

2. `TestHandleTeamCreateRejectsExistingBranch`
   - open のイシューだが、フィーチャーブランチが git リポジトリに既存
   - chatlog に拒否メッセージが記録される
   - チーム数が変化しない

3. `TestHandleTeamCreateRejectsExistingWorktree`
   - open のイシューだが、ワークツリーディレクトリが存在する
   - chatlog に拒否メッセージが記録される
   - チーム数が変化しない

## 影響範囲

- `internal/orchestrator/decision.go`：`DecideTeamAssignment` 関数
- `internal/orchestrator/orchestrator.go`：`handleTeamCreate` 関数
- `internal/orchestrator/decision_test.go`：既存テスト更新・新規テスト追加
- `internal/orchestrator/orchestrator_test.go`：新規テスト追加

## 注意事項

- `startAllTeams` は `handleTeamCreate` を経由しないため、この変更の影響を受けない
  - 起動時に in_progress イシューを正しくピックアップする動作は維持される
- ブランチチェックは IO 操作であるため、純粋関数 `DecideTeamAssignment` では行わず
  `handleTeamCreate` 内で実行する
- ブランチチェックは `DecideTeamAssignment` 呼び出し前に実行し、早期リターンする
