package agent

// messages holds localized message templates for agent communication.
type messages struct {
	// Initial prompt messages
	RoleIntro          string // "You are an agent operating in the following role."
	OriginalTaskHeader string // "## Original Request"
	MemoHeader         string // "## Recent work memo (inherited from last context reset)"
	ChatLogPathLabel   string // "Chat log path: "
	ChatLogWriteInstr  string // "Use the following command to write to the chat log:"
	WaitForMention     string // "Wait for mentions addressed to you in the chat log, and respond appropriately."
	StartImplement     string // "Follow the request above and start implementation immediately. Report to the superintendent when done. Continue monitoring the chat log for mentions and respond to additional instructions."

	// Message prompt messages
	NewMessageSingle   string // "There is a new message in the chat log:"
	NewMessageMultiple string // "There are %d new messages in the chat log:"
	RespondAll         string // "Respond appropriately to all messages."
	RespondSingle      string // "Respond appropriately."

	// Continuation prompt
	Continuation string // "Work was interrupted midway. Check the current directory state (git status, etc.) and resume the interrupted work."
}

// langMessages maps language codes to their message sets.
var langMessages = map[string]messages{
	"en": {
		RoleIntro:          "You are an agent operating in the following role.\n\n",
		OriginalTaskHeader: "## Original Request\n",
		MemoHeader:         "## Recent work memo (inherited from last context reset)\n",
		ChatLogPathLabel:   "Chat log path: ",
		ChatLogWriteInstr:  "Use the following command to write to the chat log:\n",
		WaitForMention:     "Wait for mentions addressed to you in the chat log, and respond appropriately.",
		StartImplement:     "Follow the request above and start implementation immediately. Report to the superintendent when done. Continue monitoring the chat log for mentions and respond to additional instructions.",
		NewMessageSingle:   "There is a new message in the chat log:\n\n%s\n\nRespond appropriately.",
		NewMessageMultiple: "There are %d new messages in the chat log:\n\n",
		RespondAll:         "\nRespond appropriately to all messages.",
		RespondSingle:      "Respond appropriately.",
		Continuation:       "Work was interrupted midway. Check the current directory state (git status, etc.) and resume the interrupted work.",
	},
	"ja": {
		RoleIntro:          "あなたは以下の役割で動作するエージェントです。\n\n",
		OriginalTaskHeader: "## 元の依頼内容\n",
		MemoHeader:         "## 直近の作業メモ（前回のコンテキストリセットから引き継ぎ）\n",
		ChatLogPathLabel:   "チャットログのパス: ",
		ChatLogWriteInstr:  "チャットログへの書き込みには以下のコマンドを使用してください:\n",
		WaitForMention:     "自分宛のメンションがチャットログに投稿されるのを待ち、適切に対応してください。",
		StartImplement:     "上記の依頼内容に従い、すぐに実装を開始してください。実装完了後は監督に報告してください。その後もチャットログへのメンションを監視し、追加の指示に対応してください。",
		NewMessageSingle:   "チャットログに新しいメッセージがあります:\n\n%s\n\n適切に対応してください。",
		NewMessageMultiple: "チャットログに新しいメッセージが %d 件あります:\n\n",
		RespondAll:         "\nすべてのメッセージに適切に対応してください。",
		RespondSingle:      "適切に対応してください。",
		Continuation:       "作業の途中で中断されました。現在のディレクトリの状態を確認し（git status等）、中断した作業を再開してください。",
	},
}

// getMessages returns the message set for the given language code.
// Falls back to English if the language is not supported.
func getMessages(lang string) messages {
	if m, ok := langMessages[lang]; ok {
		return m
	}
	return langMessages["en"]
}
