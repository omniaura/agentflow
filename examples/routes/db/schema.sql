CREATE TABLE todos (
    id         bigserial PRIMARY KEY,
    owner_id   text        NOT NULL,
    title      text        NOT NULL,
    done       boolean     NOT NULL DEFAULT false,
    meta       jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notes (
    id         bigserial PRIMARY KEY,
    owner_id   text        NOT NULL,
    body       text        NOT NULL,
    pinned     boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);
