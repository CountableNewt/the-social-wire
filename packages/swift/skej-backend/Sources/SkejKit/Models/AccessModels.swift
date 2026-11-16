import Foundation

public enum OAuthPurpose: String, Codable, Sendable {
    case signIn = "sign_in"
    case connectAccount = "connect_account"
    case brandConnection = "brand_connection"
    case inviteAccept = "invite_accept"
}

public enum ViewerAccountAccessKind: String, Codable, Sendable {
    case owner
    case connected
    case team
}

public struct ViewerAccountAccess: Codable, Equatable, Sendable {
    public var viewerDid: String
    public var accountDid: String
    public var accessKind: ViewerAccountAccessKind
    public var teamUri: String?
    public var capabilities: [BrandCapability]
    public var isDefault: Bool
    public var createdAt: String
    public var updatedAt: String

    public init(
        viewerDid: String,
        accountDid: String,
        accessKind: ViewerAccountAccessKind,
        teamUri: String? = nil,
        capabilities: [BrandCapability] = [],
        isDefault: Bool = false,
        createdAt: String,
        updatedAt: String
    ) {
        self.viewerDid = viewerDid
        self.accountDid = accountDid
        self.accessKind = accessKind
        self.teamUri = teamUri
        self.capabilities = capabilities
        self.isDefault = isDefault
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }
}

public enum TeamInviteStatus: String, Codable, Sendable {
    case pending
    case accepted
    case revoked
    case expired
}

public struct TeamInvite: Codable, Equatable, Sendable {
    public var id: String
    public var token: String
    public var teamUri: String
    public var teamTitle: String
    public var ownerDid: String
    public var invitedHandle: String?
    public var invitedDid: String?
    public var role: TeamRole
    public var status: TeamInviteStatus
    public var inviterDid: String
    public var acceptedDid: String?
    public var createdAt: String
    public var expiresAt: String
    public var updatedAt: String

    public init(
        id: String,
        token: String,
        teamUri: String,
        teamTitle: String,
        ownerDid: String,
        invitedHandle: String? = nil,
        invitedDid: String? = nil,
        role: TeamRole = .user,
        status: TeamInviteStatus = .pending,
        inviterDid: String,
        acceptedDid: String? = nil,
        createdAt: String,
        expiresAt: String,
        updatedAt: String
    ) {
        self.id = id
        self.token = token
        self.teamUri = teamUri
        self.teamTitle = teamTitle
        self.ownerDid = ownerDid
        self.invitedHandle = invitedHandle
        self.invitedDid = invitedDid
        self.role = role
        self.status = status
        self.inviterDid = inviterDid
        self.acceptedDid = acceptedDid
        self.createdAt = createdAt
        self.expiresAt = expiresAt
        self.updatedAt = updatedAt
    }

    public func isPending(at now: String) -> Bool {
        status == .pending && expiresAt > now
    }
}

public enum ProEntitlementStatus: String, Codable, Sendable {
    case active
    case revoked
}

public enum ProEntitlementScope: String, Codable, Sendable {
    case actor
    case team
}

public struct ProEntitlement: Codable, Equatable, Sendable {
    public var scope: ProEntitlementScope
    public var subject: String
    public var status: ProEntitlementStatus
    public var source: String
    public var grantedByDid: String?
    public var expiresAt: String?
    public var createdAt: String
    public var updatedAt: String

    public init(
        scope: ProEntitlementScope = .actor,
        subject: String,
        status: ProEntitlementStatus,
        source: String = "manual",
        grantedByDid: String? = nil,
        expiresAt: String? = nil,
        createdAt: String,
        updatedAt: String
    ) {
        self.scope = scope
        self.subject = subject
        self.status = status
        self.source = source
        self.grantedByDid = grantedByDid
        self.expiresAt = expiresAt
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    public func isActive(at now: String) -> Bool {
        status == .active && (expiresAt == nil || expiresAt! > now)
    }
}

public struct StoredProtocolRecord<Value: Codable & Sendable>: Sendable {
    public let did: String
    public let rkey: String
    public let value: Value

    public init(did: String, rkey: String, value: Value) {
        self.did = did
        self.rkey = rkey
        self.value = value
    }
}
