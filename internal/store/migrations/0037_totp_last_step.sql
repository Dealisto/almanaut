-- TOTP replay protection (RFC 6238 §5.2). VerifyTOTP accepts a code for any
-- step in a ±1 window, so without remembering which step was last accepted the
-- same code works again for about 90 seconds. last_used_step records the most
-- recent accepted step (unix time / 30); a code is only accepted for a step
-- strictly greater than it. 0 means no code has been accepted yet, and
-- SetSecret resets it to 0 because a new secret starts a fresh sequence.
ALTER TABLE user_totp ADD COLUMN last_used_step INTEGER NOT NULL DEFAULT 0;
