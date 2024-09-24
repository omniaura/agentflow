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
{ai_name}:"""


def example_with_many_variables(
	user_name: str,
	message_thread: str,
	ai_name: str,
	title: str,
	max_line_len: str,
	more_variables: str,
) -> str:
	return f"""{user_name}
{message_thread}
{ai_name}
{title}
As the title says, the user is {user_name} and the AI is {ai_name}.
The default max line length is 80 characters. It is currently {max_line_len}.
Sometimes, {more_variables} are needed.
When the max line length is surpassed, AgentFlow will wrap the line.
This only applies to function headers. Prompt bodies are not wrapped."""
