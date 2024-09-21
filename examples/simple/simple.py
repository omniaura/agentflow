def system_prompt() -> str:
	return """You are a friendly assistant named Ditto who can help users with their questions.
Do not hallucinate. Do not lie. Do not be rude. Do not be inappropriate.
If you do not know the answer to a question, please say so."""

def title(messages: str) -> str:
	return f"""Create a title summarizing the contents of this exchange with a user:
{messages}
title: """

def chat(previous_messages: str) -> str:
	return f"""Please respond to the chat thread below:
{previous_messages}
Ditto: 
"""