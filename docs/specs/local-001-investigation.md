# local-001: engineer-5 / orchestrator 同時無応答インシデント 調査報告と対策

## 概要

2026-04-17 の issue #265 実装中に engineer-5 と orchestrator が同時に無応答となり、
superintendent が fallback step 3（直接実装）を強いられた。
本ドキュメントは根本原因の分析と採用した対策を記録する。

## インシデントタイムライン

| 時刻 (JST) | 出来事 |
|-----------|--------|
| 13:16:55 | superintendent が engineer-5 に最初のナッジ（応答なし） |
| 13:40:10 | 2回目のナッジ（応答なし） |
| 14:02:22 | 1回目の TEAM_CREATE 要求 → orchestrator 無応答 |
| 14:23:59 | 2回目の TEAM_CREATE 要求 → orchestrator 無応答 |
| 14:45:24 | 3回目の TEAM_CREATE 要求 → orchestrator 無応答 |
| 15:06 | fallback step 3 開始: commit fa0c08c 直接 push, PR #266 作成 |
| 15:28 | PR #266 squash-merge; issue #265 resolved |

## 根本原因

### RC-1: 陳腐化した team 割り当て (assigned_team > 0) による TEAM_CREATE 誤拒否

`DecideTeamAssignment` は `iss.AssignedTeam > 0` の場合に無条件で Reject を返す。
engineer-5 が無応答になり再アサインが必要になっても、issue ファイルに `assigned_team = 5` が
残っていると TEAM_CREATE が「アサイン済みです (チーム 5)」で拒否される。

- 拒否メッセージ自体はチャットログに書かれるが、superintendent のプロンプトが期待する
  "チーム作成 ACK" でないため、「orchestrator が応答しない」と判断してしまった。
- 正しい手順は「TEAM_DISBAND → TEAM_CREATE」だが、その手順がプロンプトに明記されていなかった。

**fix**: `handleTeamCreate` の前処理で、`iss.AssignedTeam > 0` かつ
`!o.teams.HasIssue(issueID)` (= チームマネージャー上にチームが存在しない) の場合は
陳腐化した割り当てとみなして `AssignedTeam` をリセットしてから Decision ロジックに渡す。
これにより orchestrator 再起動なしに TEAM_CREATE が通るようになる。

### RC-2: `{{TEAMS_FILE}}` テンプレート変数が未定義

`prompts/superintendent.md` には以下のコマンドが含まれる:

```bash
cat {{TEAMS_FILE}}
```

しかし `internal/agent/prompt.go` の `substituteVars()` に `{{TEAMS_FILE}}` が存在しない。
superintendent は実行時に `cat {{TEAMS_FILE}}` というリテラルを受け取り、コマンドが失敗するため
チームの現状が確認できず、担当エンジニアへの停止通知ができなかった。

**fix**: `PromptVars.TeamsFilePath` フィールドと `{{TEAMS_FILE}}` 置換を追加する。

### RC-3: teams.toml の原子的書き込み欠如 (next_id と [[team]] エントリの不整合)

インシデント時の `teams.toml`:

```toml
next_id = 6
```

`next_id` は 6 まで進んでいるが `[[team]]` エントリが 0 件。
orchestrator の `team.Manager` は純粋にオンメモリであり、`teams.toml` を読み書きするコードが
存在しなかった。この状態では superintendent が `cat {{TEAMS_FILE}}` を実行しても
チームの実態が把握できない。

**fix**: orchestrator が `teams.toml` をアトミックに読み書きするよう実装する。
チーム作成・解散時に毎回ファイルを更新し、`next_id` と `[[team]]` エントリが常に
整合するよう保証する。

### RC-4: engineer 無応答の自動検出機構がない

engineer が90分以上無応答になっても、システム側から自動検出・エスカレーションする
仕組みがなく、superintendent の人手による気づきに依存していた。

**long-term recommendation**: 各 engineer エージェントのアクティビティを監視し、
N 分以上チャットログへの書き込みがない場合に自動で superintendent へアラートを送る
watchdog goroutine を orchestrator に追加する。(本 fix では stub のみ実装)

## 採用した対策

| 対策 | 実装箇所 |
|------|---------|
| RC-1: 陳腐化アサイン自動リセット | `internal/orchestrator/orchestrator.go` `handleTeamCreate` |
| RC-2: `{{TEAMS_FILE}}` テンプレート変数追加 | `internal/agent/prompt.go`, `internal/orchestrator/orchestrator.go` |
| RC-3: teams.toml 永続化 | `internal/team/persist.go`, `internal/team/team.go` |
| RC-3: teams.toml 不変条件テスト | `internal/team/persist_test.go` |

## teams.toml フォーマット

```toml
# 次に割り当てるチーム番号 (単調増加、ロールバックしない)
next_id = 6

[[team]]
id = 1
issue_id = "gh-265"

[[team]]
id = 2
issue_id = ""   # アイドル (issue 未割り当て)
```

### 不変条件

- `next_id` は常に `max(team.id) + 1` 以上
- `[[team]]` の各 `id` は重複しない
- `[[team]]` エントリは常に `next_id` 未満の id を持つ

## 教訓

1. TEAM_CREATE が拒否される場合、rejectionメッセージの形式を ACK と同じヘッダーにして
   superintendent が確実に認識できるようにすること。
2. engineer 無応答の再アサイン手順 (TEAM_DISBAND → TEAM_CREATE) を superintendent
   プロンプトに明記すること。
3. 状態ファイル (teams.toml) は必ずアトミックに書くこと。部分書き込みで不整合が生じる。
