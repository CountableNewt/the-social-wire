# ReadStateCore dependency snapshot

Source: https://github.com/Stygian-Tech/the-social-wire
Revision: `47368fefb52ad90e37f6d0962a87451b04a04c56`
Path: `packages/swift/ReadStateCore`

This exact snapshot preserves the backend package dependency without moving the active Apple application package out of the original repository. Its source and tests are unchanged. Refresh deliberately from a reviewed upstream revision; this is not an independent fork.

The enclosing ThinAppViewCore manifest builds this snapshot as its own `ReadStateCore` target, with its original tests as `ReadStateCoreTests`. It does not declare a filesystem package dependency, so revision-based remote consumers can resolve the complete package graph. The nested manifest is retained only as original snapshot provenance.
