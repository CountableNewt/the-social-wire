/// A response retains this owner until it finishes or is discarded. Explicit
/// release is joined; deinit is the fallback for bodies the server never starts.
final class PrivateDraftLease: Sendable {
  private let admission: EdgeDraftAdmission
  private let lease: EdgeDraftAdmission.Lease

  init(admission: EdgeDraftAdmission, lease: EdgeDraftAdmission.Lease) {
    self.admission = admission
    self.lease = lease
  }

  func release() async { await admission.release(lease) }

  deinit {
    let admission = admission
    let lease = lease
    Task { await admission.release(lease) }
  }
}
