CREATE TABLE todos (
    id         bigserial PRIMARY KEY,
    owner_id   text        NOT NULL,
    title      text        NOT NULL,
    done       boolean     NOT NULL DEFAULT false,
    meta       jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
