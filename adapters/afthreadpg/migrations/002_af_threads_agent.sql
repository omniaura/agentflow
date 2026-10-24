-- Finding an agent's thread for afthread.Directory: the owner's oldest
-- thread for the agent.
CREATE INDEX af_threads_agent ON af_threads (owner, agent, created_at, id);
