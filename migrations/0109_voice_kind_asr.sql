-- 0109: leftover kind=voice is listen (asr). Speak is a separate tts row.
-- Drop a leftover voice catalog-default if an asr default already exists so
-- the global unique index on (kind) WHERE kind_default=1 stays valid.
UPDATE provider_models SET kind_default = 0
 WHERE kind = 'voice' AND kind_default = 1
   AND EXISTS (SELECT 1 FROM provider_models AS other WHERE other.kind = 'asr' AND other.kind_default = 1);
UPDATE provider_models SET kind = 'asr' WHERE kind = 'voice';
