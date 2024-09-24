def system_prompt(ai_name: str) -> str:
	return f"""You are a friendly assistant named {ai_name} who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so."""


def title_chat(user_name: str, messages: str) -> str:
	return f"""Create a title summarizing the contents of this exchange with {user_name}:
{messages}
title: """


def chat_with_user(previous_messages: str, ai_name: str) -> str:
	return f"""Please respond to the chat thread below:
{previous_messages}
{ai_name}: """
