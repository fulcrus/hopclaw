// Package deviceauth manages device identity, trust establishment, and
// token-based authentication for external devices (mobile apps, desktop apps,
// IDE extensions) connecting to the HopClaw gateway. It provides UUID-based
// device identity, secure token generation and storage, versioned wire-format
// auth payloads, a pairing flow with 6-digit verification codes, and HTTP
// middleware for gateway integration.
package deviceauth
