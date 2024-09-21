/**
 * Returns the system prompt for the assistant.
 * @returns {string} The system prompt.
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
export function title(messages) {
    return `Create a title summarizing the contents of this exchange with a user:
${messages}
title: `;
}

/**
 * Generates a prompt for the assistant to respond to a chat thread.
 * @param {string} previousMessages - The previous messages in the chat thread.
 * @returns {string} The prompt for the assistant to respond.
 */
export function chat(previousMessages) {
    return `Please respond to the chat thread below:
${previousMessages}
Ditto: `;
}