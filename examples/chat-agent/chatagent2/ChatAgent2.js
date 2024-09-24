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
${aiName}:
`;
}
