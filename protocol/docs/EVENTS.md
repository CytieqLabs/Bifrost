# Repository events

`v1.Event` is the durable envelope for future streaming and replay. Events are
ordered by `sequence` within one repository and deduplicated by `id`.

Consumers must persist the last processed sequence only after their side effect
completes. They must tolerate repeated delivery, unknown event types, and new
payload fields. Event data uses the corresponding v1 resource shape.

The protocol defines the envelope and event names now; transport endpoints and
retention guarantees will be introduced with the server event-store milestone.
