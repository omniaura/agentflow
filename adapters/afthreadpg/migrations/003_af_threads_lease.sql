-- Thread leases for afthread.Locker: the worker running a thread's agent
-- holds it until lease_until, so one worker at a time runs it.
ALTER TABLE af_threads
    ADD COLUMN lease_holder text NOT NULL DEFAULT '',
    ADD COLUMN lease_until  timestamptz;
