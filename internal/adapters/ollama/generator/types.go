package generator

import conversationgeneration "nadir/internal/conversation/generation"

// These aliases keep the Ollama Adapter implementation and its wire tests
// readable while the generation contract remains owned by Conversation.
type Event = conversationgeneration.Event
type TokenEvent = conversationgeneration.TokenEvent
type ErrorEvent = conversationgeneration.ErrorEvent
type DoneEvent = conversationgeneration.DoneEvent
