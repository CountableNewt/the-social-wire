// Package thinstore is the thin AppView data layer: the SQLite `content_items`,
// read-mark, unread-counter, publication-scope and feed-projection store, the
// projection caches, and the PDS getRecord cache that share the standalone
// backend's SQLite file.
//
// It ports ThinAppViewCore's ThinAppViewStore / SQLiteThinAppViewStore,
// SQLiteAppViewProjectionCacheStore and the AppView edge SQLiteCache. SQL text
// and DDL follow the Swift store so the Go binary opens files the Swift binary
// wrote without rewriting data.
package thinstore
