import Foundation

public struct SkejScheduleParameters: Codable, Equatable, Sendable {
    public let accountDid: String?
    public let rkey: String

    public init(accountDid: String? = nil, rkey: String) {
        self.accountDid = accountDid
        self.rkey = rkey
    }
}

public struct SkejCreateScheduleInput: Codable, Sendable {
    public let accountDid: String?
    public let record: SkejScheduleRecord

    public init(accountDid: String? = nil, record: SkejScheduleRecord) {
        self.accountDid = accountDid
        self.record = record
    }
}

public struct SkejUpdateScheduleInput: Codable, Sendable {
    public let accountDid: String?
    public let rkey: String
    public let record: SkejScheduleRecord

    public init(accountDid: String? = nil, rkey: String, record: SkejScheduleRecord) {
        self.accountDid = accountDid
        self.rkey = rkey
        self.record = record
    }
}
