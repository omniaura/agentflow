/**
 * @returns {string}
 */
export function systemPrompt() {
	return `You are a friendly assistant named Ditto who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.`;
}

/**
 * @param {string} messages
 * @returns {string}
 */
export function createTitle(messages) {
	return `Create a title summarizing the contents of this exchange with a user:
${messages}
title: `;
}

/**
 * @param {string} previousMessages
 * @returns {string}
 */
export function chatWithUser(previousMessages) {
	return `Please respond to the chat thread below:
${previousMessages}
Ditto: `;
}
