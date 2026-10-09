# T084 - Cache Key Correctness for Multimodal

* **Status:** Completed

## Goal
Prevent cache collisions across routes and modalities (amends ADR-011).

## Scope
1. `cache.ComputeRequestHash`: include selected logical model, `OutputModalities`, attachment content digests.
2. Do not cache responses containing generated media unless the app enables it per modality (`Cache.Modalities`).
3. Tests: same prompt with different output modality or route gets different keys; same attachment bytes with different URL gets the same key.

## Acceptance Criteria
- [x] No cross-route cache hits.
- [x] Privacy purge still removes all user-scoped keys.
