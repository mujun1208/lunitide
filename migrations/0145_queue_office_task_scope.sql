-- Legacy input remains ordinary conversation input. Office tasks keep their
-- own durable supplements without changing run_id or historical receipts.
ALTER TABLE queued_user_messages ADD COLUMN office_task_id TEXT
  CHECK (office_task_id IS NULL OR
    (length(office_task_id)=26 AND substr(office_task_id,1,1) GLOB '[0-7]'
     AND office_task_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'));
CREATE INDEX ix_quem_office_scope ON queued_user_messages(session_id,office_task_id,status,seq);
