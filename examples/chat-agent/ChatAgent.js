/**
 * @param {string} aiName
 * @returns {string}
 */
export function systemPrompt(aiName) {
	return `You are a friendly assistant named ${aiName} who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.`;
}

/**
 * @param {string} userName
 * @param {string} messages
 * @returns {string}
 */
export function titleChat(userName, messages) {
	return `Create a title summarizing the contents of this exchange with ${userName}:
${messages}
title: `;
}

/**
 * @param {string} previousMessages
 * @param {string} aiName
 * @returns {string}
 */
export function chatWithUser(previousMessages, aiName) {
	return `Please respond to the chat thread below:
${previousMessages}
${aiName}: `;
}
