package ditto

import "strings"

func SystemPrompt() string {
	return `You are a friendly AI named Ditto here to help the user who is your best friend.`
}

func MainChatTemplate(
	tools string,
	examples string,
	memoryLong string,
	memoryShort string,
	scriptName string,
	scriptType string,
	userName string,
	currentTime string,
	userPrompt string,
) string {
	var b strings.Builder
	b.Grow(233 + len(tools) + 89 + len(examples) + 287 + len(memoryLong) + 276 + len(memoryShort) + 59 + len(scriptName) + 74 + len(scriptType) + 92 + len(scriptType) + 171 + len(userName) + 34 + len(currentTime) + 16 + len(userPrompt) + 8)
	b.WriteString(`The following is a conversation between an AI named Ditto and a human that are best friends. Ditto is helpful and answers factual questions correctly but maintains a friendly relationship with the human.

<?tools>
## Available Tools
`)
	b.WriteString(tools)
	b.WriteString(`
</tools>

<?examples>
## Examples of User Prompts that need tools:
-- Begin Examples --
`)
	b.WriteString(examples)
	b.WriteString(`
-- End Examples --
</examples>

## Long Term Memory
- Relevant prompt/response pairs from the user's prompt history are indexed using cosine similarity and are shown below as Long Term Memory. 
Long Term Memory Buffer (most relevant prompt/response pairs):
-- Begin Long Term Memory --
`)
	b.WriteString(memoryLong)
	b.WriteString(`
-- End Long Term Memory --

## Short Term Memory
- The most recent prompt/response pairs are shown below as Short Term Memory. This is usually 5-10 most recent prompt/response pairs.
Short Term Memory Buffer (most recent prompt/response pairs):
-- Begin Short Term Memory --
`)
	b.WriteString(memoryShort)
	b.WriteString(`
-- End Short Term Memory --

<?script>
## Current Script: `)
	b.WriteString(scriptName)
	b.WriteString(`
- If you are reading this, that means the user is currently working on a `)
	b.WriteString(scriptType)
	b.WriteString(` script. Please send any requests from the user to the respective agent/tool for the user's `)
	b.WriteString(scriptType)
	b.WriteString(` script.
- Don't send a user's prompt to the tool if they are obviously asking you something off topic to the current script or chatting with you.
</script>

User's Name: `)
	b.WriteString(userName)
	b.WriteString(`
Current Time in User's Timezone: `)
	b.WriteString(currentTime)
	b.WriteString(`
User's Prompt: `)
	b.WriteString(userPrompt)
	b.WriteString(`
Ditto:
`)
	return b.String()
}
