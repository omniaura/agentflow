export function systemPrompt(): string {
	return `You are a friendly assistant named Ditto who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.
`;
}

export function createTitle(userName: string, messageThread: string): string {
	return `Create a title summarizing the contents of this exchange with ${userName}:
${messageThread}
title: 
`;
}

export function chatWithUser(aiName: string, messageThread: string): string {
	return `You are an AI named ${aiName}. Please respond to the chat thread below:
${messageThread}
${aiName}:
`;
}
