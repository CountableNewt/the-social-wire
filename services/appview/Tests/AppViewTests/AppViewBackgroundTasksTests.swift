import Foundation
import Testing

@testable import AppView

struct AppViewBackgroundTasksTests {
  private actor Recorder {
    var calls = 0
    func record() { calls += 1 }
  }

  @Test func concurrentShutdownJoinsCallbacksExactlyOnce() async {
    let owner = AppViewBackgroundTasks()
    let recorder = Recorder()
    #expect(
      owner.register {
        try? await Task.sleep(for: .milliseconds(20))
        await recorder.record()
      })
    await withTaskGroup(of: Void.self) { group in
      for _ in 0..<16 { group.addTask { await owner.shutdown() } }
    }
    #expect(await recorder.calls == 1)
    #expect(!owner.register { await recorder.record() })
    await owner.shutdown()
    #expect(await recorder.calls == 1)
  }

  @Test func cancelledShutdownCallerStillJoinsCleanup() async {
    let owner = AppViewBackgroundTasks()
    let recorder = Recorder()
    owner.register {
      try? await Task.sleep(for: .milliseconds(20))
      await recorder.record()
    }
    let task = Task { await owner.shutdown() }
    task.cancel()
    await task.value
    #expect(await recorder.calls == 1)
  }
}
