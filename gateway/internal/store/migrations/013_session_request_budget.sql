ALTER TABLE session ADD COLUMN request_window timestamptz NOT NULL DEFAULT now();
ALTER TABLE session ADD COLUMN request_count integer NOT NULL DEFAULT 0 CHECK(request_count>=0);
