package chatagent

import "strings"

func SystemPrompt() string {
	return `You are a friendly assistant named Bob who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.`
}

func TitleChat(userName string, messages string) string {
	var b strings.Builder
	b.Grow(62 + len(userName) + 2 + len(messages) + 8)
	b.WriteString(`Create a title summarizing the contents of this exchange with `)
	b.WriteString(userName)
	b.WriteString(`:
`)
	b.WriteString(messages)
	b.WriteString(`
title: `)
	return b.String()
}

func ChatWithUser(previousMessages string, aiName string) string {
	var b strings.Builder
	b.Grow(41 + len(previousMessages) + 1 + len(aiName) + 2)
	b.WriteString(`Please respond to the chat thread below:
`)
	b.WriteString(previousMessages)
	b.WriteString(`
`)
	b.WriteString(aiName)
	b.WriteString(`: `)
	return b.String()
}
