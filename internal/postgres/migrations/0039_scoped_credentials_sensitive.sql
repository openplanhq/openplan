-- A value marked not sensitive is shown back and left unredacted in run logs
-- (#497). Every row already stored was written as write-only, and a missing
-- value means sensitive, so the default is true. The value itself stays in
-- ciphertext either way, so the run path decrypts every row the same way.
alter table scoped_credentials add column sensitive boolean not null default true;
