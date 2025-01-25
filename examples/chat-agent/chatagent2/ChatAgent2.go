package chatagent2

import "strings"

func SystemPrompt(aiName string) string {
	var b strings.Builder
	b.Grow(35 + len(aiName) + 281)
	b.WriteString(`You are a friendly assistant named `)
	b.WriteString(aiName)
	b.WriteString(` who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.
2 newlines will produce a newline in the output at the end of the prompt.
This prompt uses the technique.
`)
	return b.String()
}

func CreateTitle(userName string, messageThread string) string {
	var b strings.Builder
	b.Grow(62 + len(userName) + 2 + len(messageThread) + 8)
	b.WriteString(`Create a title summarizing the contents of this exchange with `)
	b.WriteString(userName)
	b.WriteString(`:
`)
	b.WriteString(messageThread)
	b.WriteString(`
title: `)
	return b.String()
}

func ChatWithUser(aiName string, messageThread string) string {
	var b strings.Builder
	b.Grow(20 + len(aiName) + 43 + len(messageThread) + 1 + len(aiName) + 2)
	b.WriteString(`You are an AI named `)
	b.WriteString(aiName)
	b.WriteString(`. Please respond to the chat thread below:
`)
	b.WriteString(messageThread)
	b.WriteString(`
`)
	b.WriteString(aiName)
	b.WriteString(`: `)
	return b.String()
}

func ExampleWithManyVariables(
	userName string,
	messageThread string,
	aiName string,
	title string,
	maxLineLen string,
	moreVariables string,
) string {
	var b strings.Builder
	b.Grow(len(userName) + 1 + len(messageThread) + 1 + len(aiName) + 1 + len(title) + 32 + len(userName) + 15 + len(aiName) + 64 + len(maxLineLen) + 13 + len(moreVariables) + 151)
	b.WriteString(userName)
	b.WriteString(`
`)
	b.WriteString(messageThread)
	b.WriteString(`
`)
	b.WriteString(aiName)
	b.WriteString(`
`)
	b.WriteString(title)
	b.WriteString(`
As the title says, the user is `)
	b.WriteString(userName)
	b.WriteString(` and the AI is `)
	b.WriteString(aiName)
	b.WriteString(`.
The default max line length is 80 characters. It is currently `)
	b.WriteString(maxLineLen)
	b.WriteString(`.
Sometimes, `)
	b.WriteString(moreVariables)
	b.WriteString(` are needed.
When the max line length is surpassed, AgentFlow will wrap the line.
This only applies to function headers. Prompt bodies are not wrapped.`)
	return b.String()
}

func ConditionalPrompting(userEmail string) string {
	var b strings.Builder
	b.Grow(98 + len(userEmail) + 15)
	b.WriteString(`This prompt demonstrates conditional prompting using optionals.
<?user.email>
The user's email is `)
	b.WriteString(userEmail)
	b.WriteString(`.
</user.email>`)
	return b.String()
}

func ConditionalWithElse(userSubscriptionTier string) string {
	var b strings.Builder
	b.Grow(216 + len(userSubscriptionTier) + 103)
	b.WriteString(`.var user.premium bool`)
	b.WriteString(`.var user.premium bool
.var user.subscription.tier int
Here's an example with else syntax:
<?user.premium>
Welcome premium user! You have access to all features.
Your subscription tier is level `)
	b.WriteString(userSubscriptionTier)
	b.WriteString(`.
<else>
Welcome! You're using the basic version. Upgrade to premium for more features.
</user.premium>`)
	return b.String()
}
