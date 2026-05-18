// Package media provides a unified media understanding pipeline for HopClaw.
//
// It supports image description, audio transcription, and video analysis
// using pluggable provider backends (OpenAI, Google/Gemini, Anthropic).
// The Pipeline type orchestrates concurrent processing of multiple
// attachments while the Registry manages available providers.
package media
