-- 0087 Record when a handoff was accepted.
--
-- handoff.accept promises that a repeated accept replays the original
-- effectiveAt (0063, M8-014/015). Nothing stored that moment, so the replay
-- returned created_at — when the handoff was offered — while the first accept
-- returned the acceptance time. The two agree only when both land in the same
-- RFC3339 second, so any caller that retried got a different answer than the
-- one it retried.
--
-- The sibling case already models this correctly: collab-gate decisions carry
-- confirmed_at and replay from it. accepted_at is the same column for the same
-- reason, nullable because rows accepted before this migration have no
-- recorded time and because sent/expired rows never acquire one.

ALTER TABLE handoffs ADD COLUMN accepted_at TEXT;
