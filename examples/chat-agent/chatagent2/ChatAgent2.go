package chatagent2

import "strings"

type SystemPrompt struct {
	AiName string
}

func (input *SystemPrompt) String() string {
	var b strings.Builder
	b.Grow(35 + len(input.AiName) + 281)
	b.WriteString(`You are a friendly assistant named `)
	b.WriteString(input.AiName)
	b.WriteString(` who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.
2 newlines will produce a newline in the output at the end of the prompt.
This prompt uses the technique.
`)
	return b.String()
}

type CreateTitle struct {
	UserName string
	MessageThread string
}

func (input *CreateTitle) String() string {
	var b strings.Builder
	b.Grow(62 + len(input.UserName) + 2 + len(input.MessageThread) + 8)
	b.WriteString(`Create a title summarizing the contents of this exchange with `)
	b.WriteString(input.UserName)
	b.WriteString(`:
`)
	b.WriteString(input.MessageThread)
	b.WriteString(`
title: `)
	return b.String()
}

type ChatWithUser struct {
	AiName string
	MessageThread string
}

func (input *ChatWithUser) String() string {
	var b strings.Builder
	b.Grow(20 + len(input.AiName) + 43 + len(input.MessageThread) + 1 + len(input.AiName) + 2)
	b.WriteString(`You are an AI named `)
	b.WriteString(input.AiName)
	b.WriteString(`. Please respond to the chat thread below:
`)
	b.WriteString(input.MessageThread)
	b.WriteString(`
`)
	b.WriteString(input.AiName)
	b.WriteString(`: `)
	return b.String()
}

type ExampleWithManyVariables struct {
	UserName string
	MessageThread string
	AiName string
	Title string
	MaxLineLen string
	MoreVariables string
}

func (input *ExampleWithManyVariables) String() string {
	var b strings.Builder
	b.Grow(len(input.UserName) + 1 + len(input.MessageThread) + 1 + len(input.AiName) + 1 + len(input.Title) + 32 + len(input.UserName) + 15 + len(input.AiName) + 64 + len(input.MaxLineLen) + 13 + len(input.MoreVariables) + 151)
	b.WriteString(input.UserName)
	b.WriteString(`
`)
	b.WriteString(input.MessageThread)
	b.WriteString(`
`)
	b.WriteString(input.AiName)
	b.WriteString(`
`)
	b.WriteString(input.Title)
	b.WriteString(`
As the title says, the user is `)
	b.WriteString(input.UserName)
	b.WriteString(` and the AI is `)
	b.WriteString(input.AiName)
	b.WriteString(`.
The default max line length is 80 characters. It is currently `)
	b.WriteString(input.MaxLineLen)
	b.WriteString(`.
Sometimes, `)
	b.WriteString(input.MoreVariables)
	b.WriteString(` are needed.
When the max line length is surpassed, AgentFlow will wrap the line.
This only applies to function headers. Prompt bodies are not wrapped.`)
	return b.String()
}

type ConditionalPrompting struct {
	User struct {
		Email string
	}
}

func (input *ConditionalPrompting) String() string {
	var b strings.Builder
	b.Grow(95 + len(input.User.Email) + 12)
	b.WriteString(`This prompt demonstrates conditional prompting using optionals.
`)
	b.WriteString(`user.email`)
	b.WriteString(`
The user's email is `)
	b.WriteString(input.User.Email)
	b.WriteString(`.
`)
	b.WriteString(`user.email`)
	return b.String()
}

type ConditionalWithElse struct {
	User struct {
		Premium string
		Subscription struct {
			Tier string
		}
	}
}

func (input *ConditionalWithElse) String() string {
	var b strings.Builder
	b.Grow(213 + len(input.User.Subscription.Tier) + 98)
	b.WriteString(`.var user.premium bool`)
	b.WriteString(`.var user.premium bool
.var user.subscription.tier int
Here's an example with else syntax:
`)
	b.WriteString(`user.premium`)
	b.WriteString(`
Welcome premium user! You have access to all features.
Your subscription tier is level `)
	b.WriteString(input.User.Subscription.Tier)
	b.WriteString(`.
`)
	b.WriteString(`lse>
Welcome! You're using the basic version. Upgrade to premium for more features.
`)
	b.WriteString(`user.premium`)
	return b.String()
}
