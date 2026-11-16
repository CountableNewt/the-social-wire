import Foundation

public enum ScheduleStatus: String, Codable, CaseIterable, Sendable {
    case draft
    case scheduled
    case blocked
    case publishing
    case published
    case failed
    case canceled

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        let value = try container.decode(String.self)
        self = value == "cancelled" ? .canceled : (ScheduleStatus(rawValue: value) ?? .failed)
    }
}

public enum TimezonePolicy: String, Codable, Sendable {
    case absoluteUTC = "absolute_utc"
    case accountLocal = "account_local"
    case userLocal = "user_local"
}

public enum ScheduleErrorCode: String, Codable, Sendable {
    case transientNetwork = "transient_network"
    case rateLimited = "rate_limited"
    case authInvalid = "auth_invalid"
    case recordInvalid = "record_invalid"
    case parentMissing = "parent_missing"
    case parentUnavailable = "parent_unavailable"
    case unknown
}

public struct ScheduleError: Codable, Equatable, Error, Sendable {
    public let code: ScheduleErrorCode
    public let message: String
    public let classification: ScheduleErrorCode
    public let retryAfter: String?

    public init(
        code: ScheduleErrorCode,
        message: String,
        classification: ScheduleErrorCode? = nil,
        retryAfter: String? = nil
    ) {
        self.code = code
        self.message = message
        self.classification = classification ?? code
        self.retryAfter = retryAfter
    }

    public init(from decoder: Decoder) throws {
        if let string = try? decoder.singleValueContainer().decode(String.self) {
            self.init(code: .unknown, message: string)
            return
        }
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let code = try container.decodeIfPresent(ScheduleErrorCode.self, forKey: .code) ?? .unknown
        let message = try container.decodeIfPresent(String.self, forKey: .message) ?? "Unknown schedule error"
        let classification = try container.decodeIfPresent(ScheduleErrorCode.self, forKey: .classification) ?? code
        let retryAfter = try container.decodeIfPresent(String.self, forKey: .retryAfter)
        self.init(code: code, message: message, classification: classification, retryAfter: retryAfter)
    }
}

public struct RetryState: Codable, Equatable, Sendable {
    public var attemptCount: Int
    public var lastAttemptAt: String?
    public var nextAttemptAt: String?
    public var maxAttempts: Int

    public init(attemptCount: Int = 0, lastAttemptAt: String? = nil, nextAttemptAt: String? = nil, maxAttempts: Int = 8) {
        self.attemptCount = attemptCount
        self.lastAttemptAt = lastAttemptAt
        self.nextAttemptAt = nextAttemptAt
        self.maxAttempts = maxAttempts
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        attemptCount = Self.decodeInteger(
            from: container,
            forKey: .attemptCount,
            defaultValue: 0
        )
        lastAttemptAt = try container.decodeIfPresent(String.self, forKey: .lastAttemptAt)
        nextAttemptAt = try container.decodeIfPresent(String.self, forKey: .nextAttemptAt)
        maxAttempts = Self.decodeInteger(
            from: container,
            forKey: .maxAttempts,
            defaultValue: 8
        )
    }

    private static func decodeInteger(
        from container: KeyedDecodingContainer<CodingKeys>,
        forKey key: CodingKeys,
        defaultValue: Int
    ) -> Int {
        if let value = try? container.decode(Int.self, forKey: key) {
            return value
        }
        if let legacyBoolean = try? container.decode(Bool.self, forKey: key) {
            return legacyBoolean ? 1 : 0
        }
        return defaultValue
    }

    private enum CodingKeys: String, CodingKey {
        case attemptCount
        case lastAttemptAt
        case nextAttemptAt
        case maxAttempts
    }
}

public struct ScheduleDependency: Codable, Equatable, Sendable {
    public var dependsOnScheduleUri: String
    public var relationship: ScheduleDependencyRelationship
    public var parentPublishedUri: String?
    public var parentPublishedCid: String?

    public init(
        dependsOnScheduleUri: String,
        relationship: ScheduleDependencyRelationship = .after,
        parentPublishedUri: String? = nil,
        parentPublishedCid: String? = nil
    ) {
        self.dependsOnScheduleUri = dependsOnScheduleUri
        self.relationship = relationship
        self.parentPublishedUri = parentPublishedUri
        self.parentPublishedCid = parentPublishedCid
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        dependsOnScheduleUri = try container.decode(String.self, forKey: .dependsOnScheduleUri)
        relationship = try container.decodeIfPresent(
            ScheduleDependencyRelationship.self,
            forKey: .relationship
        ) ?? .after
        parentPublishedUri = try container.decodeIfPresent(String.self, forKey: .parentPublishedUri)
        parentPublishedCid = try container.decodeIfPresent(String.self, forKey: .parentPublishedCid)
    }
}

public enum ScheduleDependencyRelationship: String, Codable, CaseIterable, Sendable {
    case after
    case reply
    case quote
}

public enum PostSourceFormat: String, Codable, CaseIterable, Sendable {
    case markdown
}

public struct ResolvedMention: Codable, Equatable, Sendable {
    public let handle: String
    public let did: String

    public init(handle: String, did: String) {
        self.handle = handle
        self.did = did
    }
}

public struct PostSource: Codable, Equatable, Sendable {
    public let format: PostSourceFormat
    public let text: String
    public let mentions: [ResolvedMention]?

    public init(format: PostSourceFormat, text: String, mentions: [ResolvedMention]? = nil) {
        self.format = format
        self.text = text
        self.mentions = mentions
    }
}

public struct PostPlan: Codable, Equatable, Sendable {
    public let text: String
    public let source: PostSource?
    public let publishRkey: String?
    public let facets: [JSONValue]?
    public let reply: JSONValue?
    public let embed: JSONValue?
    public let langs: [String]?
    public let labels: [String]?
    public let tags: [String]?
    public let unknownFields: [String: JSONValue]

    public init(
        text: String,
        source: PostSource? = nil,
        publishRkey: String? = nil,
        facets: [JSONValue]? = nil,
        reply: JSONValue? = nil,
        embed: JSONValue? = nil,
        langs: [String]? = nil,
        labels: [String]? = nil,
        tags: [String]? = nil,
        unknownFields: [String: JSONValue] = [:]
    ) {
        self.text = text
        self.source = source
        self.publishRkey = publishRkey
        self.facets = facets
        self.reply = reply
        self.embed = embed
        self.langs = langs
        self.labels = labels
        self.tags = tags
        self.unknownFields = unknownFields
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        text = try container.decode(String.self, forKey: .text)
        source = try container.decodeIfPresent(PostSource.self, forKey: .source)
        publishRkey = try container.decodeIfPresent(String.self, forKey: .publishRkey)
        facets = try container.decodeIfPresent([JSONValue].self, forKey: .facets)
        reply = try container.decodeIfPresent(JSONValue.self, forKey: .reply)
        embed = try container.decodeIfPresent(JSONValue.self, forKey: .embed)
        langs = try container.decodeIfPresent([String].self, forKey: .langs)
        labels = try container.decodeIfPresent([String].self, forKey: .labels)
        tags = try container.decodeIfPresent([String].self, forKey: .tags)

        let dynamic = try decoder.container(keyedBy: DynamicCodingKey.self)
        let knownKeys = Set(CodingKeys.allCases.map(\.rawValue))
        unknownFields = try dynamic.allKeys.reduce(into: [:]) { fields, key in
            guard !knownKeys.contains(key.stringValue) else { return }
            fields[key.stringValue] = try dynamic.decode(JSONValue.self, forKey: key)
        }
    }

    public func encode(to encoder: Encoder) throws {
        var dynamic = encoder.container(keyedBy: DynamicCodingKey.self)
        for (key, value) in unknownFields {
            try dynamic.encode(value, forKey: DynamicCodingKey(key))
        }

        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(text, forKey: .text)
        try container.encodeIfPresent(source, forKey: .source)
        try container.encodeIfPresent(publishRkey, forKey: .publishRkey)
        try container.encodeIfPresent(facets, forKey: .facets)
        try container.encodeIfPresent(reply, forKey: .reply)
        try container.encodeIfPresent(embed, forKey: .embed)
        try container.encodeIfPresent(langs, forKey: .langs)
        try container.encodeIfPresent(labels, forKey: .labels)
        try container.encodeIfPresent(tags, forKey: .tags)
    }

    private enum CodingKeys: String, CodingKey, CaseIterable {
        case text
        case source
        case publishRkey
        case facets
        case reply
        case embed
        case langs
        case labels
        case tags
    }
}

public struct LinkPreviewRequest: Codable, Equatable, Sendable {
    public let url: String

    public init(url: String) {
        self.url = url
    }
}

public struct ATProtoCIDLink: Codable, Equatable, Sendable {
    public let link: String

    public init(link: String) {
        self.link = link
    }

    enum CodingKeys: String, CodingKey {
        case link = "$link"
    }
}

public struct ATProtoBlobReference: Codable, Equatable, Sendable {
    public let type: String
    public let ref: ATProtoCIDLink
    public let mimeType: String
    public let size: Int

    public init(
        type: String = "blob",
        ref: ATProtoCIDLink,
        mimeType: String,
        size: Int
    ) {
        self.type = type
        self.ref = ref
        self.mimeType = mimeType
        self.size = size
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case ref
        case mimeType
        case size
    }
}

public struct ExternalEmbedContent: Codable, Equatable, Sendable {
    public let uri: String
    public let title: String
    public let description: String
    public let thumb: ATProtoBlobReference?

    public init(
        uri: String,
        title: String,
        description: String,
        thumb: ATProtoBlobReference? = nil
    ) {
        self.uri = uri
        self.title = title
        self.description = description
        self.thumb = thumb
    }
}

public struct ExternalEmbed: Codable, Equatable, Sendable {
    public let type: String
    public let external: ExternalEmbedContent

    public init(
        type: String = "app.bsky.embed.external",
        external: ExternalEmbedContent
    ) {
        self.type = type
        self.external = external
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case external
    }
}

public struct SkejScheduleRecord: Codable, Equatable, Sendable {
    public let type: String
    public var scheduledAt: String
    public var title: String?
    public var teamUri: String?
    public var createdByDid: String?
    public var approvedByDid: String?
    public var approvedAt: String?
    public var timezonePolicy: TimezonePolicy
    public var userTimezone: String?
    public var createdAt: String
    public var updatedAt: String
    public var status: ScheduleStatus
    public var recordType: String
    public var shadowRecord: JSONValue?
    public var publishRkey: String
    public var publishedUri: String?
    public var publishedCid: String?
    public var calendarEventUri: String?
    public var calendarEventCid: String?
    public var publishedPosts: [PublishedPostReference]
    public var retry: RetryState
    public var lastError: ScheduleError?
    public var dependency: ScheduleDependency?
    public var posts: [PostPlan]
    public var unknownFields: [String: JSONValue]

    public var scheduledFor: String {
        get { scheduledAt }
        set { scheduledAt = newValue }
    }

    public var scheduleUri: String {
        "at://unknown/at.skej.schedule/\(publishRkey)"
    }

    public init(
        type: String = "at.skej.schedule",
        scheduledAt: String,
        title: String? = nil,
        teamUri: String? = nil,
        createdByDid: String? = nil,
        approvedByDid: String? = nil,
        approvedAt: String? = nil,
        timezonePolicy: TimezonePolicy = .userLocal,
        userTimezone: String? = nil,
        createdAt: String,
        updatedAt: String,
        status: ScheduleStatus,
        recordType: String = "app.bsky.feed.post",
        shadowRecord: JSONValue? = nil,
        publishRkey: String,
        publishedUri: String? = nil,
        publishedCid: String? = nil,
        calendarEventUri: String? = nil,
        calendarEventCid: String? = nil,
        publishedPosts: [PublishedPostReference] = [],
        retry: RetryState = RetryState(),
        lastError: ScheduleError? = nil,
        dependency: ScheduleDependency? = nil,
        posts: [PostPlan],
        unknownFields: [String: JSONValue] = [:]
    ) {
        self.type = type
        self.scheduledAt = scheduledAt
        self.title = title
        self.teamUri = teamUri
        self.createdByDid = createdByDid
        self.approvedByDid = approvedByDid
        self.approvedAt = approvedAt
        self.timezonePolicy = timezonePolicy
        self.userTimezone = userTimezone
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.status = status
        self.recordType = recordType
        self.shadowRecord = shadowRecord
        self.publishRkey = publishRkey
        self.publishedUri = publishedUri
        self.publishedCid = publishedCid
        self.calendarEventUri = calendarEventUri
        self.calendarEventCid = calendarEventCid
        self.publishedPosts = publishedPosts
        self.retry = retry
        self.lastError = lastError
        self.dependency = dependency
        self.posts = posts
        self.unknownFields = unknownFields
    }

    public init(
        type: String = "at.skej.schedule",
        scheduledFor: String,
        createdAt: String,
        updatedAt: String,
        status: ScheduleStatus,
        lastError: ScheduleError? = nil,
        posts: [PostPlan]
    ) {
        self.init(
            type: type,
            scheduledAt: scheduledFor,
            createdAt: createdAt,
            updatedAt: updatedAt,
            status: status,
            publishRkey: ATProtoTID.generate(),
            lastError: lastError,
            posts: posts
        )
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.type = try container.decode(String.self, forKey: .type)
        self.scheduledAt = try container.decodeIfPresent(String.self, forKey: .scheduledAt)
            ?? container.decode(String.self, forKey: .scheduledFor)
        self.title = try container.decodeIfPresent(String.self, forKey: .title)
        self.teamUri = try container.decodeIfPresent(String.self, forKey: .teamUri)
        self.createdByDid = try container.decodeIfPresent(String.self, forKey: .createdByDid)
        self.approvedByDid = try container.decodeIfPresent(String.self, forKey: .approvedByDid)
        self.approvedAt = try container.decodeIfPresent(String.self, forKey: .approvedAt)
        self.timezonePolicy = try container.decodeIfPresent(TimezonePolicy.self, forKey: .timezonePolicy) ?? .userLocal
        self.userTimezone = try container.decodeIfPresent(String.self, forKey: .userTimezone)
        self.createdAt = try container.decode(String.self, forKey: .createdAt)
        self.updatedAt = try container.decode(String.self, forKey: .updatedAt)
        self.status = try container.decode(ScheduleStatus.self, forKey: .status)
        self.recordType = try container.decodeIfPresent(String.self, forKey: .recordType) ?? "app.bsky.feed.post"
        self.shadowRecord = try container.decodeIfPresent(JSONValue.self, forKey: .shadowRecord)
        self.publishRkey = try container.decodeIfPresent(String.self, forKey: .publishRkey) ?? ATProtoTID.generate()
        self.publishedUri = try container.decodeIfPresent(String.self, forKey: .publishedUri)
        self.publishedCid = try container.decodeIfPresent(String.self, forKey: .publishedCid)
        self.calendarEventUri = try container.decodeIfPresent(String.self, forKey: .calendarEventUri)
        self.calendarEventCid = try container.decodeIfPresent(String.self, forKey: .calendarEventCid)
        self.publishedPosts = try container.decodeIfPresent(
            [PublishedPostReference].self,
            forKey: .publishedPosts
        ) ?? []
        self.retry = try container.decodeIfPresent(RetryState.self, forKey: .retry) ?? RetryState()
        self.lastError = try container.decodeIfPresent(ScheduleError.self, forKey: .lastError)
        self.dependency = try container.decodeIfPresent(ScheduleDependency.self, forKey: .dependency)
        self.posts = try container.decodeIfPresent([PostPlan].self, forKey: .posts) ?? []

        let dynamic = try decoder.container(keyedBy: DynamicCodingKey.self)
        let knownKeys = Set(CodingKeys.allCases.map(\.rawValue))
        self.unknownFields = try dynamic.allKeys.reduce(into: [:]) { fields, key in
            guard !knownKeys.contains(key.stringValue) else { return }
            fields[key.stringValue] = try dynamic.decode(JSONValue.self, forKey: key)
        }
    }

    public func encode(to encoder: Encoder) throws {
        var dynamic = encoder.container(keyedBy: DynamicCodingKey.self)
        for (key, value) in unknownFields {
            try dynamic.encode(value, forKey: DynamicCodingKey(key))
        }
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(type, forKey: .type)
        try container.encode(scheduledAt, forKey: .scheduledAt)
        try container.encodeIfPresent(title, forKey: .title)
        try container.encodeIfPresent(teamUri, forKey: .teamUri)
        try container.encodeIfPresent(createdByDid, forKey: .createdByDid)
        try container.encodeIfPresent(approvedByDid, forKey: .approvedByDid)
        try container.encodeIfPresent(approvedAt, forKey: .approvedAt)
        try container.encode(timezonePolicy, forKey: .timezonePolicy)
        try container.encodeIfPresent(userTimezone, forKey: .userTimezone)
        try container.encode(createdAt, forKey: .createdAt)
        try container.encode(updatedAt, forKey: .updatedAt)
        try container.encode(status, forKey: .status)
        try container.encode(recordType, forKey: .recordType)
        try container.encodeIfPresent(shadowRecord, forKey: .shadowRecord)
        try container.encode(publishRkey, forKey: .publishRkey)
        try container.encodeIfPresent(publishedUri, forKey: .publishedUri)
        try container.encodeIfPresent(publishedCid, forKey: .publishedCid)
        try container.encodeIfPresent(calendarEventUri, forKey: .calendarEventUri)
        try container.encodeIfPresent(calendarEventCid, forKey: .calendarEventCid)
        if !publishedPosts.isEmpty {
            try container.encode(publishedPosts, forKey: .publishedPosts)
        }
        try container.encode(retry, forKey: .retry)
        try container.encodeIfPresent(lastError, forKey: .lastError)
        try container.encodeIfPresent(dependency, forKey: .dependency)
        try container.encode(posts, forKey: .posts)
    }

    enum CodingKeys: String, CodingKey, CaseIterable {
        case type = "$type"
        case scheduledAt
        case title
        case teamUri
        case createdByDid
        case approvedByDid
        case approvedAt
        case scheduledFor
        case timezonePolicy
        case userTimezone
        case createdAt
        case updatedAt
        case status
        case recordType
        case shadowRecord
        case publishRkey
        case publishedUri
        case publishedCid
        case calendarEventUri
        case calendarEventCid
        case publishedPosts
        case retry
        case lastError
        case dependency
        case posts
    }
}

private struct DynamicCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int? = nil

    init(_ stringValue: String) {
        self.stringValue = stringValue
    }

    init?(stringValue: String) {
        self.init(stringValue)
    }

    init?(intValue: Int) {
        return nil
    }
}

public enum TeamStatus: String, Codable, Sendable {
    case active
    case archived
}

public enum TeamRole: String, Codable, Sendable {
    case admin
    case user
}

public enum MembershipStatus: String, Codable, Sendable {
    case active
    case disabled
}

public enum TeamGroupStatus: String, Codable, Sendable {
    case active
    case archived
}

public enum BrandGrantStatus: String, Codable, Sendable {
    case active
    case revoked
}

public enum BrandCapability: String, Codable, CaseIterable, Sendable {
    case create
    case approve
    case manage
    case viewAnalytics
}

public enum GrantGranteeType: String, Codable, Sendable {
    case member
    case group
}

public struct SkejTeamRecord: Codable, Equatable, Sendable {
    public let type: String
    public var ownerAdminDid: String
    public var title: String
    public var status: TeamStatus
    public var createdAt: String
    public var updatedAt: String

    public init(
        type: String = "at.skej.team",
        ownerAdminDid: String,
        title: String,
        status: TeamStatus = .active,
        createdAt: String,
        updatedAt: String
    ) {
        self.type = type
        self.ownerAdminDid = ownerAdminDid
        self.title = title
        self.status = status
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case ownerAdminDid
        case title
        case status
        case createdAt
        case updatedAt
    }
}

public struct TeamMemberRecord: Codable, Equatable, Sendable {
    public let type: String
    public var teamUri: String
    public var memberDid: String
    public var role: TeamRole
    public var status: MembershipStatus
    public var groupUris: [String]
    public var createdAt: String
    public var updatedAt: String

    public init(
        type: String = "at.skej.team.member",
        teamUri: String,
        memberDid: String,
        role: TeamRole,
        status: MembershipStatus = .active,
        groupUris: [String] = [],
        createdAt: String,
        updatedAt: String
    ) {
        self.type = type
        self.teamUri = teamUri
        self.memberDid = memberDid
        self.role = role
        self.status = status
        self.groupUris = groupUris
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case teamUri
        case memberDid
        case role
        case status
        case groupUris
        case createdAt
        case updatedAt
    }
}

public struct TeamGroupRecord: Codable, Equatable, Sendable {
    public let type: String
    public var teamUri: String
    public var name: String
    public var memberDids: [String]
    public var brandGrantUris: [String]
    public var status: TeamGroupStatus
    public var createdAt: String
    public var updatedAt: String

    public init(
        type: String = "at.skej.team.group",
        teamUri: String,
        name: String,
        memberDids: [String] = [],
        brandGrantUris: [String] = [],
        status: TeamGroupStatus = .active,
        createdAt: String,
        updatedAt: String
    ) {
        self.type = type
        self.teamUri = teamUri
        self.name = name
        self.memberDids = memberDids
        self.brandGrantUris = brandGrantUris
        self.status = status
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case teamUri
        case name
        case memberDids
        case brandGrantUris
        case status
        case createdAt
        case updatedAt
    }

    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        type = try values.decodeIfPresent(String.self, forKey: .type) ?? "at.skej.team.group"
        teamUri = try values.decode(String.self, forKey: .teamUri)
        name = try values.decode(String.self, forKey: .name)
        memberDids = try values.decodeIfPresent([String].self, forKey: .memberDids) ?? []
        brandGrantUris = try values.decodeIfPresent([String].self, forKey: .brandGrantUris) ?? []
        status = try values.decodeIfPresent(TeamGroupStatus.self, forKey: .status) ?? .active
        createdAt = try values.decode(String.self, forKey: .createdAt)
        updatedAt = try values.decode(String.self, forKey: .updatedAt)
    }
}

public struct BrandGrantRecord: Codable, Equatable, Sendable {
    public let type: String
    public var teamUri: String
    public var brandDid: String
    public var granteeType: GrantGranteeType
    public var grantee: String
    public var capabilities: [BrandCapability]
    public var status: BrandGrantStatus
    public var createdAt: String
    public var updatedAt: String

    public init(
        type: String = "at.skej.team.brandGrant",
        teamUri: String,
        brandDid: String,
        granteeType: GrantGranteeType,
        grantee: String,
        capabilities: [BrandCapability],
        status: BrandGrantStatus = .active,
        createdAt: String,
        updatedAt: String
    ) {
        self.type = type
        self.teamUri = teamUri
        self.brandDid = brandDid
        self.granteeType = granteeType
        self.grantee = grantee
        self.capabilities = capabilities
        self.status = status
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case teamUri
        case brandDid
        case granteeType
        case grantee
        case capabilities
        case status
        case createdAt
        case updatedAt
    }

    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        type = try values.decodeIfPresent(String.self, forKey: .type) ?? "at.skej.team.brandGrant"
        teamUri = try values.decode(String.self, forKey: .teamUri)
        brandDid = try values.decode(String.self, forKey: .brandDid)
        granteeType = try values.decode(GrantGranteeType.self, forKey: .granteeType)
        grantee = try values.decode(String.self, forKey: .grantee)
        capabilities = try values.decodeIfPresent([BrandCapability].self, forKey: .capabilities) ?? []
        status = try values.decodeIfPresent(BrandGrantStatus.self, forKey: .status) ?? .active
        createdAt = try values.decode(String.self, forKey: .createdAt)
        updatedAt = try values.decode(String.self, forKey: .updatedAt)
    }
}

public struct SkejBrandRecord: Codable, Equatable, Sendable {
    public let type: String
    public var teamUri: String
    public var ownerAdminDid: String
    public var brandDid: String
    public var status: ManagedAccountStatus
    public var createdAt: String
    public var updatedAt: String

    public init(
        type: String = "at.skej.brand",
        teamUri: String,
        ownerAdminDid: String,
        brandDid: String,
        status: ManagedAccountStatus = .active,
        createdAt: String,
        updatedAt: String
    ) {
        self.type = type
        self.teamUri = teamUri
        self.ownerAdminDid = ownerAdminDid
        self.brandDid = brandDid
        self.status = status
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case teamUri
        case ownerAdminDid
        case brandDid
        case status
        case createdAt
        case updatedAt
    }
}

public struct ManagedAccount: Codable, Equatable, Sendable {
    public var did: String
    public var handle: String?
    public var displayName: String?
    public var avatar: String?
    public var pdsEndpoint: String?
    public var status: ManagedAccountStatus
    public var isDefault: Bool

    public init(did: String, handle: String? = nil, displayName: String? = nil, avatar: String? = nil, pdsEndpoint: String? = nil, status: ManagedAccountStatus = .active, isDefault: Bool = false) {
        self.did = did
        self.handle = handle
        self.displayName = displayName
        self.avatar = avatar
        self.pdsEndpoint = pdsEndpoint
        self.status = status
        self.isDefault = isDefault
    }
}

public enum ManagedAccountStatus: String, Codable, Sendable {
    case active
    case needsReauth = "needs_reauth"
    case disabled
}

public struct ScheduledJob: Codable, Equatable, Sendable {
    public let did: String
    public let rkey: String
    public var scheduledAt: String
    public var status: ScheduleStatus
    public var attempts: Int
    public var lastError: ScheduleError?
    public var nextAttemptAt: String?
    public var lastAttemptAt: String?
    public var publishRkey: String
    public var recordType: String
    public var publishedUri: String?
    public var publishedCid: String?
    public var dependsOnScheduleUri: String?
    public var parentPublishedUri: String?

    public var scheduledFor: String {
        get { scheduledAt }
        set { scheduledAt = newValue }
    }

    public init(
        did: String,
        rkey: String,
        scheduledAt: String,
        status: ScheduleStatus,
        attempts: Int,
        lastError: ScheduleError? = nil,
        nextAttemptAt: String? = nil,
        lastAttemptAt: String? = nil,
        publishRkey: String,
        recordType: String = "app.bsky.feed.post",
        publishedUri: String? = nil,
        publishedCid: String? = nil,
        dependsOnScheduleUri: String? = nil,
        parentPublishedUri: String? = nil
    ) {
        self.did = did
        self.rkey = rkey
        self.scheduledAt = scheduledAt
        self.status = status
        self.attempts = attempts
        self.lastError = lastError
        self.nextAttemptAt = nextAttemptAt
        self.lastAttemptAt = lastAttemptAt
        self.publishRkey = publishRkey
        self.recordType = recordType
        self.publishedUri = publishedUri
        self.publishedCid = publishedCid
        self.dependsOnScheduleUri = dependsOnScheduleUri
        self.parentPublishedUri = parentPublishedUri
    }

    public init(
        did: String,
        rkey: String,
        scheduledFor: String,
        status: ScheduleStatus,
        attempts: Int,
        lastError: ScheduleError? = nil,
        publishedUri: String? = nil,
        publishedCid: String? = nil
    ) {
        self.init(
            did: did,
            rkey: rkey,
            scheduledAt: scheduledFor,
            status: status,
            attempts: attempts,
            lastError: lastError,
            publishRkey: rkey,
            publishedUri: publishedUri,
            publishedCid: publishedCid
        )
    }
}

public struct ScheduledPostSummary: Codable, Equatable, Sendable {
    public let rkey: String
    public let did: String
    public let scheduleUri: String
    public let scheduledAt: String
    public let status: ScheduleStatus
    public let record: SkejScheduleRecord
    public let attempts: Int
    public let lastError: ScheduleError?
    public let nextAttemptAt: String?
    public let publishedUri: String?
    public let publishedCid: String?

    public var scheduledFor: String { scheduledAt }

    public init(rkey: String, did: String, scheduleUri: String, scheduledAt: String, status: ScheduleStatus, record: SkejScheduleRecord, attempts: Int, lastError: ScheduleError? = nil, nextAttemptAt: String? = nil, publishedUri: String? = nil, publishedCid: String? = nil) {
        self.rkey = rkey
        self.did = did
        self.scheduleUri = scheduleUri
        self.scheduledAt = scheduledAt
        self.status = status
        self.record = record
        self.attempts = attempts
        self.lastError = lastError
        self.nextAttemptAt = nextAttemptAt
        self.publishedUri = publishedUri
        self.publishedCid = publishedCid
    }
}

public struct TeamSummary: Codable, Equatable, Sendable {
    public let rkey: String
    public let uri: String
    public var record: SkejTeamRecord

    public init(rkey: String, uri: String, record: SkejTeamRecord) {
        self.rkey = rkey; self.uri = uri; self.record = record
    }
}

public struct TeamMemberSummary: Codable, Equatable, Sendable {
    public let rkey: String
    public let uri: String
    public let record: TeamMemberRecord

    public init(rkey: String, uri: String, record: TeamMemberRecord) {
        self.rkey = rkey; self.uri = uri; self.record = record
    }
}

public struct TeamGroupSummary: Codable, Equatable, Sendable {
    public let rkey: String
    public let uri: String
    public let record: TeamGroupRecord

    public init(rkey: String, uri: String, record: TeamGroupRecord) {
        self.rkey = rkey; self.uri = uri; self.record = record
    }
}

public struct BrandGrantSummary: Codable, Equatable, Sendable {
    public let rkey: String
    public let uri: String
    public let record: BrandGrantRecord

    public init(rkey: String, uri: String, record: BrandGrantRecord) {
        self.rkey = rkey; self.uri = uri; self.record = record
    }
}

public struct BrandSummary: Codable, Equatable, Sendable {
    public let rkey: String
    public let uri: String
    public let record: SkejBrandRecord

    public init(rkey: String, uri: String, record: SkejBrandRecord) {
        self.rkey = rkey; self.uri = uri; self.record = record
    }
}

public struct EffectiveBrandPermission: Codable, Equatable, Sendable {
    public let brandDid: String
    public let capabilities: [BrandCapability]

    public init(brandDid: String, capabilities: [BrandCapability]) {
        self.brandDid = brandDid
        self.capabilities = capabilities
    }
}

public struct BrandProfile: Codable, Equatable, Sendable {
    public var did: String
    public var handle: String?
    public var displayName: String?
    public var description: String?
    public var avatar: String?

    public init(did: String, handle: String? = nil, displayName: String? = nil, description: String? = nil, avatar: String? = nil) {
        self.did = did
        self.handle = handle
        self.displayName = displayName
        self.description = description
        self.avatar = avatar
    }
}

public struct AuditEvent: Codable, Equatable, Sendable {
    public let id: String
    public let did: String
    public let scheduleRkey: String?
    public let action: String
    public let message: String
    public let createdAt: String
}

public struct Viewer: Codable, Equatable, Sendable {
    public let did: String
    public let handle: String?
    public let displayName: String?
    public let avatar: String?
    public let defaultAccountDid: String?
    public let proFeaturesEnabled: Bool?

    public init(
        did: String,
        handle: String? = nil,
        displayName: String? = nil,
        avatar: String? = nil,
        defaultAccountDid: String? = nil,
        proFeaturesEnabled: Bool? = nil
    ) {
        self.did = did
        self.handle = handle
        self.displayName = displayName
        self.avatar = avatar
        self.defaultAccountDid = defaultAccountDid ?? did
        self.proFeaturesEnabled = proFeaturesEnabled
    }
}

public struct CreateScheduleRequest: Codable, Sendable {
    public let record: SkejScheduleRecord

    public init(record: SkejScheduleRecord) {
        self.record = record
    }
}

public struct CreateTeamRequest: Codable, Sendable {
    public let title: String

    public init(title: String) {
        self.title = title
    }
}

public struct UpdateTeamRequest: Codable, Sendable {
    public let title: String?
    public let status: TeamStatus?

    public init(title: String? = nil, status: TeamStatus? = nil) {
        self.title = title
        self.status = status
    }
}

public struct TransferTeamOwnerRequest: Codable, Sendable {
    public let ownerAdminDid: String

    public init(ownerAdminDid: String) {
        self.ownerAdminDid = ownerAdminDid
    }
}

public struct UpsertMemberRequest: Codable, Sendable {
    public let memberDid: String
    public let role: TeamRole
    public let status: MembershipStatus?
    public let groupUris: [String]?

    public init(memberDid: String, role: TeamRole, status: MembershipStatus? = nil, groupUris: [String]? = nil) {
        self.memberDid = memberDid
        self.role = role
        self.status = status
        self.groupUris = groupUris
    }
}

public struct UpsertGroupRequest: Codable, Sendable {
    public let name: String
    public let memberDids: [String]?
    public let brandGrantUris: [String]?
    public let status: TeamGroupStatus?

    public init(name: String, memberDids: [String]? = nil, brandGrantUris: [String]? = nil, status: TeamGroupStatus? = nil) {
        self.name = name
        self.memberDids = memberDids
        self.brandGrantUris = brandGrantUris
        self.status = status
    }
}

public struct UpsertBrandGrantRequest: Codable, Sendable {
    public let brandDid: String
    public let granteeType: GrantGranteeType
    public let grantee: String
    public let capabilities: [BrandCapability]
    public let status: BrandGrantStatus?

    public init(brandDid: String, granteeType: GrantGranteeType, grantee: String, capabilities: [BrandCapability], status: BrandGrantStatus? = nil) {
        self.brandDid = brandDid
        self.granteeType = granteeType
        self.grantee = grantee
        self.capabilities = capabilities
        self.status = status
    }
}

public struct UpsertBrandRequest: Codable, Sendable {
    public let brandDid: String
    public let status: ManagedAccountStatus?

    public init(brandDid: String, status: ManagedAccountStatus? = nil) {
        self.brandDid = brandDid
        self.status = status
    }
}

public struct UpdateBrandProfileRequest: Codable, Sendable {
    public let displayName: String?
    public let description: String?
    public let avatar: String?

    public init(displayName: String? = nil, description: String? = nil, avatar: String? = nil) {
        self.displayName = displayName
        self.description = description
        self.avatar = avatar
    }
}

public struct ListSchedulesResponse: Codable, Sendable {
    public let records: [ScheduledPostSummary]
    public init(records: [ScheduledPostSummary]) { self.records = records }
}

public struct ListTeamsResponse: Codable, Sendable {
    public let teams: [TeamSummary]
    public init(teams: [TeamSummary]) { self.teams = teams }
}

public struct ListMembersResponse: Codable, Sendable {
    public let members: [TeamMemberSummary]
    public init(members: [TeamMemberSummary]) { self.members = members }
}

public struct ListGroupsResponse: Codable, Sendable {
    public let groups: [TeamGroupSummary]
    public init(groups: [TeamGroupSummary]) { self.groups = groups }
}

public struct ListBrandGrantsResponse: Codable, Sendable {
    public let grants: [BrandGrantSummary]
    public init(grants: [BrandGrantSummary]) { self.grants = grants }
}

public struct ListBrandsResponse: Codable, Sendable {
    public let brands: [BrandSummary]
    public init(brands: [BrandSummary]) { self.brands = brands }
}

public struct ListAccountsResponse: Codable, Sendable {
    public let accounts: [ManagedAccount]
    public init(accounts: [ManagedAccount]) { self.accounts = accounts }
}

public struct ListAuditEventsResponse: Codable, Sendable {
    public let events: [AuditEvent]
    public init(events: [AuditEvent]) { self.events = events }
}

public struct OKResponse: Codable, Sendable {
    public let ok: Bool

    public init(ok: Bool) {
        self.ok = ok
    }
}

public struct PublishedPost: Codable, Equatable, Sendable {
    public let uri: String
    public let cid: String

    public init(uri: String, cid: String) {
        self.uri = uri
        self.cid = cid
    }
}

/// An ordered published-thread entry retained on the schedule record.
///
/// `PublishedPost` remains the PDS client's immediate write result; this type also
/// carries the stable record key needed to resume or reconcile multi-post publishes.
public struct PublishedPostReference: Codable, Equatable, Sendable {
    public let rkey: String
    public let uri: String
    public let cid: String

    public init(rkey: String, uri: String, cid: String) {
        self.rkey = rkey
        self.uri = uri
        self.cid = cid
    }
}

public enum ATURI {
    public static func record(did: String, collection: String, rkey: String) -> String {
        "at://\(did)/\(collection)/\(rkey)"
    }

    public static func schedule(did: String, rkey: String) -> String {
        record(did: did, collection: "at.skej.schedule", rkey: rkey)
    }

    public static func published(did: String, recordType: String, publishRkey: String) -> String {
        "at://\(did)/\(recordType)/\(publishRkey)"
    }
}

public enum ULID {
    private static let alphabet = Array("0123456789ABCDEFGHJKMNPQRSTVWXYZ")

    public static func generate(date: Date = Date()) -> String {
        var value = UInt64(date.timeIntervalSince1970 * 1000)
        var time = Array(repeating: Character("0"), count: 10)
        for index in stride(from: 9, through: 0, by: -1) {
            time[index] = alphabet[Int(value % 32)]
            value /= 32
        }

        var random = ""
        for _ in 0..<16 {
            random.append(alphabet[Int.random(in: 0..<alphabet.count)])
        }
        return String(time) + random
    }
}

public enum ATProtoTID {
    private static let alphabet = Array("234567abcdefghijklmnopqrstuvwxyz")
    private static let clockID = UInt64.random(in: 0..<1_024)
    private static let lock = NSLock()
    nonisolated(unsafe) private static var lastValue: UInt64 = 0

    public static func generate(date: Date = Date()) -> String {
        let seconds = max(date.timeIntervalSince1970, 0)
        let micros = UInt64(seconds * 1_000_000)
        let candidate = (micros << 10) | clockID

        lock.lock()
        let value = max(candidate, lastValue &+ 1)
        lastValue = value
        lock.unlock()

        var encoded = Array(repeating: Character("2"), count: 13)
        var remaining = value
        for index in stride(from: 12, through: 0, by: -1) {
            encoded[index] = alphabet[Int(remaining & 31)]
            remaining >>= 5
        }
        return String(encoded)
    }

    public static func isValid(_ value: String) -> Bool {
        guard value.count == 13,
              let first = value.first,
              "234567abcdefghij".contains(first)
        else { return false }
        return value.allSatisfy { alphabet.contains($0) }
    }
}
