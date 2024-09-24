export function systemPrompt(aiName: string): string {
	return `You are a friendly assistant named ${aiName} who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.`;
}

export function titleChat(userName: string, messages: string): string {
	return `Create a title summarizing the contents of this exchange with ${userName}:
${messages}
title: `;
}

export function chatWithUser(previousMessages: string, aiName: string): string {
	return `Please respond to the chat thread below:
${previousMessages}
${aiName}: `;
}
