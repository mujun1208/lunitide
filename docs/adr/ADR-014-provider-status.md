# ADR-014: Provider status

## Status
Accepted

## Decision
Until lifecycle requirements are expanded, a provider has the minimal explicit states `enabled` and `disabled`. Status is independent of credential availability. Deletion remains a timestamped soft delete rather than a status.

Credential state also includes `requires_reentry`; origin or protocol changes may never silently reuse the old credential reference.
