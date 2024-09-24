/**
 * @returns {string}
 */
export function systemPrompt() {
	return `You are a friendly assistant named Ditto who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.
`;
}

/**
 * @param {string} userName
 * @param {string} messageThread
 * @returns {string}
 */
export function createTitle(userName, messageThread) {
	return `Create a title summarizing the contents of this exchange with ${userName}:
${messageThread}
title: 
`;
}

/**
 * @param {string} aiName
 * @param {string} messageThread
 * @returns {string}
 */
export function chatWithUser(aiName, messageThread) {
	return `You are an AI named ${aiName}. Please respond to the chat thread below:
${messageThread}
${aiName}:`;
}

/**
 * @param {string} userName
 * @param {string} messageThread
 * @param {string} aiName
 * @param {string} title
 * @param {string} maxLineLen
 * @param {string} moreVariables
 * @returns {string}
 */
export function exampleWithManyVariables(
	userName,
	messageThread,
	aiName,
	title,
	maxLineLen,
	moreVariables,
) {
	return `${userName}
${messageThread}
${aiName}
${title}
As the title says, the user is ${userName} and the AI is ${aiName}.
The default max line length is 80 characters. It is currently ${maxLineLen}.
Sometimes, ${moreVariables} are needed.
When the max line length is surpassed, AgentFlow will wrap the line.
This only applies to function headers. Prompt bodies are not wrapped.`;
}
