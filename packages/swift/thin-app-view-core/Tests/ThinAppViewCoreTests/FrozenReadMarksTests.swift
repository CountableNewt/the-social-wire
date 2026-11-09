import Foundation
import GRDB
import Logging
import Testing
import ThinAppViewCore

@Suite struct FrozenReadMarksTests {
  @Test func preservesUnreadChoicesNewerThanFrozenSelection() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "frozen-read-test"))
    let action = Date(timeIntervalSince1970: 1_700_000_000)
    try await store.markEntryUnread(viewerDid: "viewer", subjectUri: "old", createdAt: action.addingTimeInterval(-1))
    try await store.markEntryUnread(viewerDid: "viewer", subjectUri: "new", createdAt: action.addingTimeInterval(1))
    let applied = try await store.applyFrozenReadMarks(viewerDid: "viewer", subjectUris: ["old", "new", "old"], createdAt: action)
    #expect(applied == ["old"])
    #expect(try await store.hasReadMark(viewerDid: "viewer", subjectUri: "old"))
    #expect(try await !store.hasReadMark(viewerDid: "viewer", subjectUri: "new"))
    let database = try DatabasePool(path: path)
    let unread = try await database.read { db in
      try String.fetchAll(db, sql: "SELECT subject_uri FROM appview_unread_overrides WHERE viewer_did = ?", arguments: ["viewer"])
    }
    #expect(unread == ["new"])
  }

  @Test func doesNotReplaceNewerReadTimestamp() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "frozen-read-test"))
    let earlier = Date(timeIntervalSince1970: 1_700_000_000)
    let later = earlier.addingTimeInterval(60)
    try await store.upsertReadMark(viewerDid: "viewer", subjectUri: "entry", createdAt: later)
    #expect(try await store.applyFrozenReadMarks(viewerDid: "viewer", subjectUris: ["entry", "entry"], createdAt: earlier) == ["entry"])
    let database = try DatabasePool(path: path)
    let timestamp = try await database.read { db in
      try String.fetchOne(db, sql: "SELECT created_at FROM read_marks WHERE viewer_did = ? AND subject_uri = ?", arguments: ["viewer", "entry"])
    }
    #expect(timestamp.flatMap { ISO8601DateFormatter().date(from: $0) } == later)
  }

  @Test func rollsBackEntireFrozenSelectionOnLaterFailure() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "frozen-read-test"))
    let database = try DatabasePool(path: path)
    try await database.write { db in
      try db.execute(sql: """
        CREATE TRIGGER fail_frozen BEFORE INSERT ON read_marks WHEN NEW.subject_uri = 'subject-0500'
        BEGIN SELECT RAISE(ABORT, 'injected'); END;
        """)
    }
    let subjects = (0..<501).map { String(format: "subject-%04d", $0) }
    await #expect(throws: DatabaseError.self) {
      try await store.applyFrozenReadMarks(viewerDid: "viewer", subjectUris: subjects, createdAt: Date())
    }
    #expect(try await database.read { db in try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM read_marks") } == 0)
  }
}
