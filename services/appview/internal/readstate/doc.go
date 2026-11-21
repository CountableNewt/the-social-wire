// Package readstate ports ReadStateCore and the ThinAppViewCore PDS read-state
// pieces: the app.thesocialwire.readState manifest and chunk record models,
// their validation and CID verification, the verified projection, the SQLite
// authority/projection store, frozen read marks, idle eviction and resumable
// repository recovery.
package readstate
