export function systemPrompt(): string {
	return `You are HelloBot, a simple assistant that loves to say hello to users.
Your only purpose is to greet users in a friendly way and say "Hello, World!" with some variation.
Keep your responses short, cheerful, and hello-focused.
`;
}

export function createTitle(messages: string): string {
	return `Create a simple hello-themed title for this exchange:
${messages}
title:
`;
}

export function chatWithUser(previousMessages: string): string {
	return `Say hello to the user in a cheerful way:
${previousMessages}
HelloBot:
`;
}
