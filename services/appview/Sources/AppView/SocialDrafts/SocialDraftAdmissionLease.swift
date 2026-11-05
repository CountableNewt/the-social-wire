final class SocialDraftAdmissionLease: Sendable {
  private let release: @Sendable () -> Void
  init(release: @escaping @Sendable () -> Void) { self.release = release }
  deinit { release() }
}
