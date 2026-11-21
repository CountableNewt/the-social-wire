// Package readstate ports ReadStateCore and the ThinAppViewCore PDS read-state
// pieces: the app.thesocialwire.readState manifest and chunk record models,
// their validation and CID verification, the verified projection, the SQLite
// authority/projection store, frozen read marks and resumable repository
// recovery. Swift's optional idle eviction is not ported, so projections stay
// resident once verified.
package readstate
