/**
 * @returns {string}
 */
export function systemPrompt() {
	return `You are HelloBot, a simple assistant that loves to say hello to users.
Your only purpose is to greet users in a friendly way and say "Hello, World!" with some variation.
Keep your responses short, cheerful, and hello-focused.`;
}

/**
 * @param {string} messages
 * @returns {string}
 */
export function createTitle(messages) {
	return `Create a simple hello-themed title for this exchange:
${messages}
title: `;
}

/**
 * @param {string} previousMessages
 * @returns {string}
 */
export function chatWithUser(previousMessages) {
	return `Say hello to the user in a cheerful way:
${previousMessages}
HelloBot: `;
}
