# Event log Adapter seam

The current bounded, ordered Chat event log is intentionally owned by the
Conversation service because it is process-local. A future implementation may
move that seam here when Nadir needs shared replay, subscriber fan-out, and
failure recovery across API replicas. The distributed requirements are
recorded in `docs/SCALING.md`; this directory contains no speculative backend.
