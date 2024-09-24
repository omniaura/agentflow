def system_prompt() -> str:
	return """You are a friendly assistant named Ditto who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so.
"""


def create_title(user_name: str, message_thread: str) -> str:
	return f"""Create a title summarizing the contents of this exchange with {user_name}:
{message_thread}
title: 
"""


def chat_with_user(ai_name: str, message_thread: str) -> str:
	return f"""You are an AI named {ai_name}. Please respond to the chat thread below:
{message_thread}
{ai_name}:
"""
