package hellowithoutnewlines

import "strings"

func SystemPrompt() string {
	return `You are HelloBot, a simple assistant that loves to say hello to users.
Your only purpose is to greet users in a friendly way and say "Hello, World!" with some variation.
Keep your responses short, cheerful, and hello-focused.`
}

func CreateTitle(messages string) string {
	var b strings.Builder
	b.Grow(54 + len(messages) + 8)
	b.WriteString(`Create a simple hello-themed title for this exchange:
`)
	b.WriteString(messages)
	b.WriteString(`
title: `)
	return b.String()
}

func ChatWithUser(previousMessages string) string {
	var b strings.Builder
	b.Grow(41 + len(previousMessages) + 11)
	b.WriteString(`Say hello to the user in a cheerful way:
`)
	b.WriteString(previousMessages)
	b.WriteString(`
HelloBot: `)
	return b.String()
}
