CREATE TABLE todos (
    id         bigserial PRIMARY KEY,
    owner_id   text        NOT NULL,
    title      text        NOT NULL,
    done       boolean     NOT NULL DEFAULT false,
    meta       jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, title)
);

CREATE TABLE notes (
    id         bigserial PRIMARY KEY,
    owner_id   text        NOT NULL,
    -- The database validates what a client or a model may write: a
    -- rejected value is a 400 naming the column, or a tool error the model
    -- can fix.
    body       text        NOT NULL CHECK (body <> '') CHECK (length(body) <= 2000),
    pinned     boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
